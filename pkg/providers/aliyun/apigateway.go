package aliyun

import (
	"fmt"
	apigw "github.com/alibabacloud-go/cloudapi-20160714/v5/client"
	cloudapi "github.com/alibabacloud-go/cloudapi-20160714/v5/client"
	openapi "github.com/alibabacloud-go/darabonba-openapi/v2/client"
	rc "github.com/alibabacloud-go/resourcecenter-20221201/client"
	"github.com/alibabacloud-go/tea/tea"
	"github.com/projectdiscovery/gologger"
	"github.com/wgpsec/lc/pkg/schema"
	"strings"
)

type apiGatewayProvider struct {
	id           string
	provider     string
	config       providerConfig
	apiGWRegions *cloudapi.DescribeRegionsResponse
}

type ApisMeta struct {
	GroupId    string
	ResourceId string
	RegionId   string
}

var apiGwList = schema.NewResources()

func (a *apiGatewayProvider) newApiGwConfig(region string) *openapi.Config {
	endpoint := fmt.Sprintf("apigateway.%s.aliyuncs.com", region)
	return &openapi.Config{
		AccessKeyId:     &a.config.accessKeyID,
		AccessKeySecret: &a.config.accessKeySecret,
		SecurityToken:   &a.config.sessionToken,
		Endpoint:        &endpoint,
		RegionId:        &region,
	}
}

// GetResource 2024.9.8 目前资源中心无法直接获取到 apigateway 分组 id, 只能获取 api 列表中的 api 及其 区域
// 所以这里没法用资源中心的调用加速了, 毕竟
// 如果是有分组，其实就会有 域名, 如果没有 api, 从用户定义上来说请求不到任何东西 (除非有一些阿里云内置的路径)
//
// 不过从为了获取对外暴露域名这个视角, 还是一个个枚举 region 更全, 毕竟上面说的情况用走资源中心会有漏的, 虽然效率慢了点点
func (a *apiGatewayProvider) GetResource() (*schema.Resources, error) {
	var apiGwMs []ApisMeta
	var regions []string
	apiGwResources, err := a.availableApiGwResource()
	if err != nil {
		for _, region := range a.apiGWRegions.Body.Regions.Region {
			regions = append(regions, *region.RegionId)
		}

		apiGwMs, err = a.describeApiGwMeta(regions)
		if err != nil {
			gologger.Debug().Msgf("调用 apigateway describeApiGwMeta 失败: %v\n", err)
			return apiGwList, nil
		}
	} else {
		for _, apiGwR := range apiGwResources {
			apiGwMs = append(apiGwMs, ApisMeta{*apiGwR.ResourceGroupId, *apiGwR.ResourceId, *apiGwR.RegionId})
		}
	}

	err = a.describeApiGroupAttr(apiGwMs)
	if err != nil {
		return apiGwList, err
	}

	return apiGwList, nil

}

func (a *apiGatewayProvider) availableApiGwResource() ([]*rc.SearchResourcesResponseBodyResources, error) {
	rConfig := NewResourceCenterConfig(a.config.accessKeyID, a.config.accessKeySecret, a.config.sessionToken)
	rClient, err := rc.NewClient(rConfig)
	if err != nil {
		return nil, err
	}

	filter0 := &rc.SearchResourcesRequestFilter{
		Key:   tea.String("ResourceType"),
		Value: []*string{tea.String("ACS::ApiGateway::Api")},
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

func (a *apiGatewayProvider) describeApiGwMeta(regions []string) ([]ApisMeta, error) {
	var apiGwMs []ApisMeta
	for _, region := range regions {
		apiGwConfig := a.newApiGwConfig(region)
		apiGwClient, err := apigw.NewClient(apiGwConfig)
		if err != nil {
			return nil, err
		}

		agApisReq := &cloudapi.DescribeApisRequest{}
		apiGwApisResp, err := apiGwClient.DescribeApis(agApisReq)
		if err != nil {
			return nil, err
		}

		for _, agApi := range apiGwApisResp.Body.ApiSummarys.ApiSummary {
			apiGwMs = append(apiGwMs, ApisMeta{*agApi.GroupId, *agApi.ApiId, region})
		}

		maxPage := *apiGwApisResp.Body.TotalCount
		for i := int32(1); i < maxPage; i++ {
			agApisReq.PageNumber = &i
			apiGwApisResp, err = apiGwClient.DescribeApis(agApisReq)
			if err != nil {
				return nil, err
			}
			for _, agApi := range apiGwApisResp.Body.ApiSummarys.ApiSummary {
				apiGwMs = append(apiGwMs, ApisMeta{*agApi.GroupId, *agApi.ApiId, region})
			}
		}
	}

	return apiGwMs, nil
}

func (a *apiGatewayProvider) describeApiAttr(apisMs []ApisMeta) error {
	for _, apim := range apisMs {
		apiGwConfig := a.newApiGwConfig(apim.RegionId)
		apiGwClient, err := apigw.NewClient(apiGwConfig)
		if err != nil {
			return err
		}

		lbaReq := &apigw.DescribeApiRequest{}
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
