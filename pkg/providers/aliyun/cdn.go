package aliyun

import (
	"fmt"
	cdn "github.com/alibabacloud-go/cdn-20180510/v5/client"
	openapi "github.com/alibabacloud-go/darabonba-openapi/v2/client"
	"github.com/aliyun/alibaba-cloud-sdk-go/services/sts"
	"github.com/projectdiscovery/gologger"
	"github.com/wgpsec/lc/pkg/schema"
)

type cdnProvider struct {
	id       string
	identity *sts.GetCallerIdentityResponse
	provider string
	config   providerConfig
}

var cdnList = schema.NewResources()

func (c *cdnProvider) newClient() (*cdn.Client, error) {
	endpoint := "cdn.aliyuncs.com"
	client := &openapi.Config{
		AccessKeyId:     &c.config.accessKeyID,
		AccessKeySecret: &c.config.accessKeySecret,
		Endpoint:        &endpoint,
	}

	cc, err := cdn.NewClient(client)
	return cc, err
}

func (c *cdnProvider) GetResource() (*schema.Resources, error) {
	cdnClient, err := c.newClient()
	if err != nil {
		gologger.Debug().Msgf("初始化 cdn client 失败: %v\f", err)
		return nil, err
	}
	gologger.Debug().Msg("正在获取阿里云 CDN 资源信息")
	for {
		dudReq := &cdn.DescribeUserDomainsRequest{}
		domains, err := cdnClient.DescribeUserDomains(dudReq)
		if err != nil {
			gologger.Debug().Msgf("调用 cdn DescribeUserDomains 失败: %v\f", err)
			break
		}
		fmt.Println(domains.Body.Domains)
		break
	}

	return cdnList, nil
}
