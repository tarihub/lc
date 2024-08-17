package aliyun

import (
	"fmt"
	openapi "github.com/alibabacloud-go/darabonba-openapi/v2/client"
	dcdn "github.com/alibabacloud-go/dcdn-20180115/v3/client"
	"github.com/aliyun/alibaba-cloud-sdk-go/services/sts"
	"github.com/projectdiscovery/gologger"
	"github.com/wgpsec/lc/pkg/schema"
	"strings"
)

type dcdnProvider struct {
	id       string
	identity *sts.GetCallerIdentityResponse
	provider string
	config   providerConfig
	client   *dcdn.Client
}

var dcdnList = schema.NewResources()

func (c *dcdnProvider) newClient() (*dcdn.Client, error) {
	endpoint := "dcdn.aliyuncs.com"
	client := &openapi.Config{
		AccessKeyId:     &c.config.accessKeyID,
		AccessKeySecret: &c.config.accessKeySecret,
		Endpoint:        &endpoint,
	}

	cc, err := dcdn.NewClient(client)
	return cc, err
}

func (c *dcdnProvider) GetResource() (*schema.Resources, error) {
	var err error
	c.client, err = c.newClient()
	if err != nil {
		gologger.Debug().Msgf("初始化 dcdn client 失败: %v\n", err)
		return nil, err
	}
	gologger.Debug().Msg("正在获取阿里云 DCDN 资源信息")

	userDomains, err := c.describeDcdnUserDomains()
	if err != nil {
		gologger.Debug().Msgf("调用 dcdn DescribeUserDomains 失败: %v\n", err)
		return nil, err
	}

	availableDomains, err := c.describeAvailableCname(userDomains)
	if err != nil {
		return nil, err
	}
	if len(availableDomains) == 0 {
		gologger.Debug().Msgf("未获取到解析 cname 的 dcdn")
		return dcdnList, nil
	}

	for _, ud := range availableDomains {
		if strings.ToLower(*ud.DomainStatus) != "online" {
			gologger.Debug().Msgf("dcdn userDomain 状态为 [%s], 跳过\n", *ud.DomainStatus)
			continue
		}
		privateOss := false
		for _, s := range ud.Sources.Source {
			// 暂时只识别私有 OSS
			if strings.ToLower(*s.Type) != "oss" {
				continue
			}
			// 查看是否开启私有回源
			if ok := c.describeCdnDomainConfig(ud.DomainName); !ok {
				continue
			}
			dcdnList.Append(&schema.Resource{
				ID:       c.id,
				Provider: c.provider,
				DNSName:  fmt.Sprintf("http://%s/#private-oss_%s", *ud.DomainName, *s.Content),
				Public:   true,
			})
			privateOss = true
		}

		if !privateOss {
			dcdnList.Append(&schema.Resource{
				ID:       c.id,
				Provider: c.provider,
				DNSName:  fmt.Sprintf("http://%s", *ud.DomainName),
				Public:   true,
			})
		}
	}

	return dcdnList, nil
}

func (c *dcdnProvider) describeAvailableCname(userDomains []*dcdn.DescribeDcdnUserDomainsResponseBodyDomainsPageData) (
	[]*dcdn.DescribeDcdnUserDomainsResponseBodyDomainsPageData, error,
) {
	domains := make([]string, 0)
	for _, ud := range userDomains {
		domains = append(domains, *ud.DomainName)
	}

	// 定义批次大小
	batch := 30
	cnames := make([]*dcdn.DescribeDcdnDomainCnameResponseBodyCnameDatasData, 0)
	for i := 0; i < len(domains); i += batch {
		end := i + batch
		if end > len(domains) {
			end = len(domains)
		}

		batchStr := strings.Join(domains[i:end], ",")

		// 先获取 DescribeDomainCname 其中 Cname 不为空字符串表示已配置 每次最多查询30个
		ddcReq := &dcdn.DescribeDcdnDomainCnameRequest{DomainName: &batchStr}
		cnameRes, err := c.client.DescribeDcdnDomainCname(ddcReq)
		if err != nil {
			gologger.Debug().Msgf("调用 cdn DescribeDomainCname req: %v 失败: %v\n", ddcReq, err)
			return nil, err
		}
		cnames = append(cnames, cnameRes.Body.CnameDatas.Data...)
	}

	availableDomains := make([]*dcdn.DescribeDcdnUserDomainsResponseBodyDomainsPageData, 0)
	for idx, cname := range cnames {
		if *cname.Cname != "" {
			availableDomains = append(availableDomains, userDomains[idx])
		}
	}

	return availableDomains, nil
}

func (c *dcdnProvider) describeDcdnUserDomains() ([]*dcdn.DescribeDcdnUserDomainsResponseBodyDomainsPageData, error) {
	var pageSize32 int32 = 200
	ddudReq := &dcdn.DescribeDcdnUserDomainsRequest{PageSize: &pageSize32}
	userDomainRes, err := c.client.DescribeDcdnUserDomains(ddudReq)
	if err != nil {
		return nil, err
	}

	pageSize := *userDomainRes.Body.PageSize
	totalPages := (*userDomainRes.Body.TotalCount / pageSize) + 1

	userDomains := userDomainRes.Body.Domains.PageData

	// 分页读取所有域名, 这里有多个强转是因为 阿里云官方的 sdk 有点奇怪, 请求参数和返回值 同一个参数, 类型不一样...
	for currentPage := 2; int64(currentPage) <= totalPages; currentPage++ {
		currentPage32 := int32(currentPage)
		ddudReq.PageNumber = &currentPage32
		userDomainRes, err = c.client.DescribeDcdnUserDomains(ddudReq)
		if err != nil {
			return nil, err
		}
		userDomains = append(userDomains, userDomainRes.Body.Domains.PageData...)
	}

	return userDomains, nil
}

func (c *dcdnProvider) describeCdnDomainConfig(domainName *string) bool {
	fn := "l2_oss_key"
	cdcReq := dcdn.DescribeDcdnDomainConfigsRequest{FunctionNames: &fn, DomainName: domainName}
	domainConfigs, err := c.client.DescribeDcdnDomainConfigs(&cdcReq)
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
