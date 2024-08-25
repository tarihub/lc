package aliyun

import (
	openapi "github.com/alibabacloud-go/darabonba-openapi/v2/client"
	rc "github.com/alibabacloud-go/resourcecenter-20221201/client"
	"github.com/projectdiscovery/gologger"
)

type ResourceMeta struct {
	ResourceId string
	RegionId   string
}

func NewResourceCenterConfig(ak, sk, st string) *openapi.Config {
	endpoint := "resourcecenter.aliyuncs.com"
	return &openapi.Config{
		AccessKeyId:     &ak,
		AccessKeySecret: &sk,
		SecurityToken:   &st,
		Endpoint:        &endpoint,
	}
}

func SearchResource(client *rc.Client, srReq *rc.SearchResourcesRequest) (resources []*rc.SearchResourcesResponseBodyResources, err error) {
	for {
		sResult, err := client.SearchResources(srReq)
		if err != nil {
			gologger.Debug().Msgf("%v SearchResources err: %v\n", srReq, err)
			return nil, err
		}

		resources = append(resources, sResult.Body.Resources...)

		if sResult.Body.NextToken == nil {
			break
		}
		gologger.Debug().Msgf("NextToken 不为空，正在获取下一页数据")
		srReq.NextToken = sResult.Body.NextToken

	}
	return resources, nil
}
