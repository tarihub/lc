package aliyun

import (
	"fmt"
	apigw "github.com/alibabacloud-go/cloudapi-20160714/v5/client"
	cloudapi "github.com/alibabacloud-go/cloudapi-20160714/v5/client"
	openapi "github.com/alibabacloud-go/darabonba-openapi/v2/client"
	"github.com/projectdiscovery/gologger"
	"github.com/wgpsec/lc/pkg/schema"
	"sync"
)

type apiGatewayProvider struct {
	id           string
	provider     string
	config       providerConfig
	apiGWRegions *cloudapi.DescribeRegionsResponse
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
	var (
		threads int
		err     error
		wg      sync.WaitGroup
		regions []string
	)
	threads = schema.GetThreads()

	for _, region := range a.apiGWRegions.Body.Regions.Region {
		regions = append(regions, *region.RegionId)
	}

	taskCh := make(chan string, threads)
	for i := 0; i < threads; i++ {
		wg.Add(1)
		go func() {
			err = a.describeApiGroup(taskCh, &wg)
			if err != nil {
				return
			}
		}()
	}

	for _, item := range regions {
		taskCh <- item
	}
	close(taskCh)
	wg.Wait()
	return apiGwList, nil

}

func (a *apiGatewayProvider) describeApiGroup(ch <-chan string, wg *sync.WaitGroup) error {
	defer wg.Done()

	for region := range ch {
		apiGwConfig := a.newApiGwConfig(region)
		apiGwClient, err := apigw.NewClient(apiGwConfig)
		if err != nil {
			return err
		}

		agIds, err := a.describeApiGroupIds(apiGwClient)
		if err != nil {
			gologger.Debug().Msgf("调用 describeApiGroupIds 失败: %v\n", err)
			return err
		}

		for _, agId := range agIds {
			agReq := &cloudapi.DescribeApiGroupRequest{GroupId: agId}
			agResp, err := apiGwClient.DescribeApiGroup(agReq)
			if err != nil {
				return err
			}
			ag := agResp.Body
			if len(ag.CustomDomains.DomainItem) == 0 {
				apiGwList.Append(&schema.Resource{
					ID:       a.id,
					Public:   true,
					DNSName:  *ag.SubDomain,
					Provider: a.provider,
				})
				continue
			}

			for _, domain := range ag.CustomDomains.DomainItem {
				apiGwList.Append(&schema.Resource{
					ID:       a.id,
					Public:   true,
					URL:      fmt.Sprintf("%s/#apigateway_%s", *domain.DomainName, *apiGwClient.RegionId),
					Provider: a.provider,
				})
			}
		}
	}

	return nil
}

func (a *apiGatewayProvider) describeApiGroupIds(apiGwClient *apigw.Client) ([]*string, error) {
	agIds := make([]*string, 0)

	agsReq := &cloudapi.DescribeApiGroupsRequest{}
	agsResp, err := apiGwClient.DescribeApiGroups(agsReq)
	if err != nil {
		return nil, err
	}

	for _, aga := range agsResp.Body.ApiGroupAttributes.ApiGroupAttribute {
		agIds = append(agIds, aga.GroupId)
	}

	pageSize := *agsResp.Body.PageSize
	totalPages := (*agsResp.Body.TotalCount / pageSize) + 1

	for currentPage := int32(1); currentPage < totalPages; currentPage++ {
		agsReq.PageNumber = &currentPage
		agsResp, err = apiGwClient.DescribeApiGroups(agsReq)
		if err != nil {
			return nil, err
		}
		for _, aga := range agsResp.Body.ApiGroupAttributes.ApiGroupAttribute {
			agIds = append(agIds, aga.GroupId)
		}
	}

	return agIds, nil
}
