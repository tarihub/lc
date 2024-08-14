package aliyun

import (
	"fmt"
	cdn "github.com/alibabacloud-go/cdn-20180510/v5/client"
	openapi "github.com/alibabacloud-go/darabonba-openapi/v2/client"
	"github.com/aliyun/alibaba-cloud-sdk-go/services/sts"
	"github.com/projectdiscovery/gologger"
	"github.com/wgpsec/lc/pkg/schema"
	"strings"
)

type cdnProvider struct {
	id       string
	identity *sts.GetCallerIdentityResponse
	provider string
	config   providerConfig
	client   *cdn.Client
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
	var err error
	c.client, err = c.newClient()
	if err != nil {
		gologger.Debug().Msgf("初始化 cdn client 失败: %v\n", err)
		return nil, err
	}
	gologger.Debug().Msg("正在获取阿里云 CDN 资源信息")

	userDomains, err := c.describeCdnUserDomains()
	if err != nil {
		gologger.Debug().Msgf("调用 cdn DescribeUserDomains 失败: %v\n", err)
		return nil, err
	}

	for _, ud := range userDomains {
		if strings.ToLower(*ud.DomainStatus) != "online" {
			gologger.Debug().Msgf("userDomain 状态为 [%s], 跳过\n", *ud.DomainStatus)
			continue
		}
		for _, s := range ud.Sources.Source {
			// 暂时只识别私有 OSS
			if strings.ToLower(*s.Type) != "oss" {
				continue
			}
			// 查看是否开启私有回源
			if ok := c.describeCdnDomainConfig(ud.DomainName); !ok {
				continue
			}
			cdnList.Append(&schema.Resource{
				ID:       c.id,
				Provider: c.provider,
				DNSName:  fmt.Sprintf("http://%s/#private-oss_%s", *ud.DomainName, *s.Content),
				Public:   true,
			})
		}
	}

	return cdnList, nil
}

func (c *cdnProvider) describeCdnUserDomains() ([]*cdn.DescribeUserDomainsResponseBodyDomainsPageData, error) {
	var pageSize32 int32 = 200
	dudReq := &cdn.DescribeUserDomainsRequest{PageSize: &pageSize32}
	userDomainRes, err := c.client.DescribeUserDomains(dudReq)
	if err != nil {
		return nil, err
	}

	pageSize := *userDomainRes.Body.PageSize
	totalPages := (*userDomainRes.Body.TotalCount / pageSize) + 1

	userDomains := userDomainRes.Body.Domains.PageData

	// 分页读取所有域名, 这里有多个强转是因为 阿里云官方的 sdk 有点奇怪, 请求参数和返回值 同一个参数, 类型不一样...
	for currentPage := 2; int64(currentPage) <= totalPages; currentPage++ {
		currentPage32 := int32(currentPage)
		dudReq.PageNumber = &currentPage32
		userDomainRes, err = c.client.DescribeUserDomains(dudReq)
		if err != nil {
			return nil, err
		}
		userDomains = append(userDomains, userDomainRes.Body.Domains.PageData...)
	}

	return userDomains, nil
}

func (c *cdnProvider) describeCdnDomainConfig(domainName *string) bool {
	fn := "l2_oss_key"
	cdcReq := cdn.DescribeCdnDomainConfigsRequest{FunctionNames: &fn, DomainName: domainName}
	domainConfigs, err := c.client.DescribeCdnDomainConfigs(&cdcReq)
	if err != nil {
		gologger.Debug().Msgf("调用 cdn DescribeCdnDomainConfigs req: %v 失败: %v\n", cdcReq, err)
		return false
	}

	for _, dc := range domainConfigs.Body.DomainConfigs.DomainConfig {
		for _, fa := range dc.FunctionArgs.FunctionArg {
			if strings.ToLower(*fa.ArgName) != "private_oss_auth" {
				continue
			} // perm_private_oss_tbl ak/sk, aliyun_id ...,
			// private_oss_ram_unauthorized https://help.aliyun.com/zh/cdn/user-guide/grant-alibaba-cloud-cdn-access-permissions-on-private-oss-buckets
			if strings.ToLower(*fa.ArgValue) == "on" {
				return true
			}
		}
	}

	return false
}
