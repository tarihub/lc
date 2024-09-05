package aliyun

import (
	"fmt"
	openapi "github.com/alibabacloud-go/darabonba-openapi/v2/client"
	rc "github.com/alibabacloud-go/resourcecenter-20221201/client"
	slb "github.com/alibabacloud-go/slb-20140515/v4/client"
	"github.com/alibabacloud-go/tea/tea"
	"github.com/projectdiscovery/gologger"
	"github.com/wgpsec/lc/pkg/schema"
	"strings"
)

type classicLoadBalancerProvider struct {
	id         string
	provider   string
	config     providerConfig
	clbRegions *slb.DescribeRegionsResponse
}

var clbList = schema.NewResources()

func (c *classicLoadBalancerProvider) GetResource() (*schema.Resources, error) {
	var clbMs []ResourceMeta
	var regions []string
	clbResources, err := c.availableClbResource()
	if err != nil {
		for _, region := range c.clbRegions.Body.Regions.Region {
			regions = append(regions, *region.RegionId)
		}

		clbMs, err = c.describeClbMeta(regions)
		if err != nil {
			gologger.Debug().Msgf("调用 clb describeClbMeta 失败: %v\n", err)
			return esList, nil
		}
	} else {
		for _, clbr := range clbResources {
			clbMs = append(clbMs, ResourceMeta{*clbr.ResourceId, *clbr.RegionId})
		}
	}

	err = c.describeClbAttr(clbMs)
	if err != nil {
		return clbList, err
	}

	return clbList, nil
}

func (c *classicLoadBalancerProvider) describeClbAttr(clbMs []ResourceMeta) error {
	for _, clbm := range clbMs {
		clbConfig := c.newSlbConfig(clbm.RegionId)
		clbClient, err := slb.NewClient(clbConfig)
		if err != nil {
			return err
		}

		lbaReq := &slb.DescribeLoadBalancerAttributeRequest{LoadBalancerId: &clbm.ResourceId}
		clbAttrRes, err := clbClient.DescribeLoadBalancerAttribute(lbaReq)
		if err != nil {
			return err
		}
		clbAttr := clbAttrRes.Body

		if clbAttr.AddressType == nil || clbAttr.Address == nil || clbAttr.LoadBalancerStatus == nil {
			gologger.Debug().Msgf("[skip] %s %s AddressType: %v, Address: %v, LoadBalancerStatus: %v\n",
				clbm.RegionId, clbm.ResourceId, clbAttr.AddressType, clbAttr.Address, clbAttr.LoadBalancerStatus,
			)
			return nil
		}
		if strings.ToLower(*clbAttr.LoadBalancerStatus) != "active" || strings.ToLower(*clbAttr.AddressType) != "internet" {
			continue
		}
		clbList.Append(&schema.Resource{
			ID:         c.id,
			Public:     true,
			PublicIPv4: *clbAttr.Address,
			Provider:   c.provider,
		})
	}

	return nil
}

func (c *classicLoadBalancerProvider) newSlbConfig(region string) *openapi.Config {
	endpoint := fmt.Sprintf("slb.%s.aliyuncs.com", region)
	return &openapi.Config{
		AccessKeyId:     &c.config.accessKeyID,
		AccessKeySecret: &c.config.accessKeySecret,
		SecurityToken:   &c.config.sessionToken,
		Endpoint:        &endpoint,
		RegionId:        &region,
	}
}

func (c *classicLoadBalancerProvider) availableClbResource() ([]*rc.SearchResourcesResponseBodyResources, error) {
	rConfig := NewResourceCenterConfig(c.config.accessKeyID, c.config.accessKeySecret, c.config.sessionToken)
	rClient, err := rc.NewClient(rConfig)
	if err != nil {
		return nil, err
	}

	filter0 := &rc.SearchResourcesRequestFilter{
		Key:   tea.String("ResourceType"),
		Value: []*string{tea.String("ACS::SLB::LoadBalancer")},
	}
	srReq := &rc.SearchResourcesRequest{
		Filter:     []*rc.SearchResourcesRequestFilter{filter0},
		MaxResults: tea.Int32(50),
	}
	resources, err := SearchResource(rClient, srReq)
	if err != nil {
		return nil, err
	}

	return resources, nil
}

func (c *classicLoadBalancerProvider) describeClbMeta(regions []string) ([]ResourceMeta, error) {
	var clbMs []ResourceMeta
	for _, region := range regions {
		clbConfig := c.newSlbConfig(region)
		clbClient, err := slb.NewClient(clbConfig)
		if err != nil {
			return nil, err
		}

		lbReq := &slb.DescribeLoadBalancersRequest{RegionId: &region}
		lbsResp, err := clbClient.DescribeLoadBalancers(lbReq)
		if err != nil {
			return nil, err
		}

		for _, lb := range lbsResp.Body.LoadBalancers.LoadBalancer {
			clbMs = append(clbMs, ResourceMeta{*lb.LoadBalancerId, region})
		}

		maxPage := *lbsResp.Body.TotalCount
		for i := int32(1); i < maxPage; i++ {
			lbReq.PageNumber = &i
			lbsResp, err = clbClient.DescribeLoadBalancers(lbReq)
			if err != nil {
				return nil, err
			}
			for _, lb := range lbsResp.Body.LoadBalancers.LoadBalancer {
				clbMs = append(clbMs, ResourceMeta{*lb.LoadBalancerId, region})
			}
		}
	}

	return clbMs, nil
}
