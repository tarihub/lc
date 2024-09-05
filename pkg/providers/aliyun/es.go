package aliyun

import (
	"context"
	"fmt"
	openapi "github.com/alibabacloud-go/darabonba-openapi/v2/client"
	es "github.com/alibabacloud-go/elasticsearch-20170613/v3/client"
	rc "github.com/alibabacloud-go/resourcecenter-20221201/client"
	"github.com/alibabacloud-go/tea/tea"
	"github.com/projectdiscovery/gologger"
	"github.com/wgpsec/lc/pkg/schema"
	"strings"
)

type elasticSearchProvider struct {
	id        string
	provider  string
	config    providerConfig
	esRegions *es.DescribeRegionsResponse
}

var esList = schema.NewResources()

func (e *elasticSearchProvider) GetEsResource(ctx context.Context) (*schema.Resources, error) {
	var ems []ResourceMeta
	var regions []string
	esResources, err := e.availableEsResource()
	if err != nil {
		regions = regions[:0]
		for _, region := range e.esRegions.Body.Result {
			regions = append(regions, *region.RegionId)
		}

		ems, err = e.describeEsMeta(regions)
		if err != nil {
			gologger.Debug().Msgf("调用 es describeEsMeta 失败: %v\n", err)
			return esList, nil
		}
	} else {
		for _, esr := range esResources {
			ems = append(ems, ResourceMeta{*esr.ResourceId, *esr.RegionId})
		}
	}

	err = e.describeEsDetail(ems)
	if err != nil {
		return esList, err
	}

	return esList, nil
}

func (e *elasticSearchProvider) describeEsDetail(ems []ResourceMeta) error {
	for _, em := range ems {
		esConfig := e.newEsConfig(em.RegionId)
		esClient, err := es.NewClient(esConfig)
		if err != nil {
			return err
		}

		esDetailRes, err := esClient.DescribeInstance(&em.ResourceId)
		if err != nil {
			return err
		}
		esDetail := esDetailRes.Body.Result

		if esDetail.PublicDomain == nil || esDetail.PublicPort == nil || esDetail.Protocol == nil {
			gologger.Debug().Msgf("[skip] %s %s Protocol: %v, PublicDomain: %v, PublicPort: %v\n",
				em.RegionId, em.ResourceId, esDetail.PublicDomain, esDetail.PublicPort, esDetail.Protocol,
			)
			return nil
		}
		esPublicUrl := fmt.Sprintf("%s://%s:%d/", strings.ToLower(*esDetail.Protocol), *esDetail.PublicDomain, *esDetail.PublicPort)
		esList.Append(&schema.Resource{
			ID:       e.id,
			Public:   true,
			URL:      esPublicUrl,
			Provider: e.provider,
		})

		if esDetail.KibanaDomain == nil || esDetail.KibanaPort == nil {
			gologger.Debug().Msgf("[skip] %s %s KibanaDomain: %v, KibanaPort: %v\n",
				em.RegionId, em.ResourceId, esDetail.KibanaDomain, esDetail.KibanaPort,
			)
			return nil
		}
		kibanaPublicUrl := fmt.Sprintf("%s:%d", *esDetail.KibanaDomain, *esDetail.KibanaPort)
		esList.Append(&schema.Resource{
			ID:       e.id,
			Public:   true,
			DNSName:  kibanaPublicUrl,
			Provider: e.provider,
		})
	}

	return nil
}

func (e *elasticSearchProvider) describeEsMeta(regions []string) ([]ResourceMeta, error) {
	var ems []ResourceMeta
	for _, region := range regions {
		esConfig := e.newEsConfig(region)
		esClient, err := es.NewClient(esConfig)
		if err != nil {
			return nil, err
		}

		liReq := &es.ListInstanceRequest{}
		instances, err := esClient.ListInstance(liReq)
		if err != nil {
			return nil, err
		}

		for _, esi := range instances.Body.Result {
			ems = append(ems, ResourceMeta{*esi.InstanceId, region})
		}

		maxPage := *instances.Body.Headers.XTotalCount
		for i := int32(1); i < maxPage; i++ {
			liReq.Page = &i
			instances, err = esClient.ListInstance(liReq)
			if err != nil {
				return nil, err
			}
			for _, esi := range instances.Body.Result {
				ems = append(ems, ResourceMeta{*esi.InstanceId, region})
			}
		}
	}

	return ems, nil
}

func (e *elasticSearchProvider) newEsConfig(region string) *openapi.Config {
	endpoint := fmt.Sprintf("elasticsearch.%s.aliyuncs.com", region)
	return &openapi.Config{
		AccessKeyId:     &e.config.accessKeyID,
		AccessKeySecret: &e.config.accessKeySecret,
		SecurityToken:   &e.config.sessionToken,
		Endpoint:        &endpoint,
		RegionId:        &region,
	}
}

// availableRegions 有两种获取 region 的方式, 如果枚举式会比较慢, 走资源中心会比较快，但需要的 ak 权限需要资源中心/管理的权限
func (e *elasticSearchProvider) availableEsResource() ([]*rc.SearchResourcesResponseBodyResources, error) {
	rConfig := NewResourceCenterConfig(e.config.accessKeyID, e.config.accessKeySecret, e.config.sessionToken)
	rClient, err := rc.NewClient(rConfig)
	if err != nil {
		return nil, err
	}

	filter0 := &rc.SearchResourcesRequestFilter{
		Key:   tea.String("ResourceType"),
		Value: []*string{tea.String("ACS::Elasticsearch::Instance")},
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
