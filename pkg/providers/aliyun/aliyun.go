package aliyun

import (
	"context"
	"fmt"
	apigw "github.com/alibabacloud-go/cloudapi-20160714/v5/client"
	es "github.com/alibabacloud-go/elasticsearch-20170613/v3/client"
	slb "github.com/alibabacloud-go/slb-20140515/v4/client"
	"github.com/aliyun/alibaba-cloud-sdk-go/sdk"
	"github.com/aliyun/alibaba-cloud-sdk-go/sdk/auth/credentials"
	"github.com/aliyun/alibaba-cloud-sdk-go/services/ecs"
	"github.com/aliyun/alibaba-cloud-sdk-go/services/rds"
	"github.com/aliyun/alibaba-cloud-sdk-go/services/sts"
	"github.com/aliyun/aliyun-oss-go-sdk/oss"
	"github.com/projectdiscovery/gologger"
	"github.com/wgpsec/lc/pkg/schema"
	"github.com/wgpsec/lc/utils"
	"strings"
)

type Provider struct {
	id           string
	provider     string
	config       providerConfig
	ossClient    *oss.Client
	apiGwRegions *apigw.DescribeRegionsResponse
	clbRegions   *slb.DescribeRegionsResponse
	ecsRegions   *ecs.DescribeRegionsResponse
	esRegions    *es.DescribeRegionsResponse
	rdsRegions   *rds.DescribeRegionsResponse
	fcRegions    []FcRegion
	identity     *sts.GetCallerIdentityResponse
}

type providerConfig struct {
	accessKeyID     string
	accessKeySecret string
	sessionToken    string
	okST            bool
	includeService  map[string]bool
}

func New(options schema.OptionBlock) (*Provider, error) {
	var (
		region    = "cn-beijing"
		ecsClient *ecs.Client
		rdsClient *rds.Client
		stsClient *sts.Client
		err       error
	)
	accessKeyID, ok := options.GetMetadata(utils.AccessKey)
	if !ok {
		return nil, &utils.ErrNoSuchKey{Name: utils.AccessKey}
	}
	accessKeySecret, ok := options.GetMetadata(utils.SecretKey)
	if !ok {
		return nil, &utils.ErrNoSuchKey{Name: utils.SecretKey}
	}
	id, _ := options.GetMetadata(utils.Id)
	sessionToken, okST := options.GetMetadata(utils.SessionToken)

	isMap := make(map[string]bool)
	includeService := ""
	if includeService, ok = options.GetMetadata(utils.IncludeService); ok {
		srv := strings.Split(includeService, ",")
		for _, s := range srv {
			isMap[strings.ToLower(s)] = true
		}
	} else {
		isMap[utils.AllService] = true
	}

	gologger.Info().Msgf("%s (%s) 配置文件指定列出的服务: %s", utils.Aliyun, id, includeService)

	config := providerConfig{
		accessKeyID:     accessKeyID,
		accessKeySecret: accessKeySecret,
		sessionToken:    sessionToken,
		okST:            okST,
		includeService:  isMap,
	}
	if okST {
		gologger.Debug().Msg("找到阿里云访问临时访问凭证")
	} else {
		gologger.Debug().Msg("找到阿里云访问永久访问凭证")
	}

	// sts GetCallerIdentity
	stsConfig := sdk.NewConfig()
	if okST {
		credential := credentials.NewStsTokenCredential(accessKeyID, accessKeySecret, sessionToken)
		stsClient, err = sts.NewClientWithOptions(region, stsConfig, credential)
	} else {
		credential := credentials.NewAccessKeyCredential(accessKeyID, accessKeySecret)
		stsClient, err = sts.NewClientWithOptions(region, stsConfig, credential)
	}
	if err != nil {
		return nil, err
	}

	stsReq := sts.CreateGetCallerIdentityRequest()
	stsReq.SetScheme("HTTPS")
	identity, err := stsClient.GetCallerIdentity(stsReq)
	if err != nil {
		return nil, err
	}
	gologger.Debug().Msg("阿里云 STS 信息获取成功")

	// apigateway client
	apiGwProvider := &apiGatewayProvider{id: id, provider: utils.Aliyun, config: config}
	apiGwConfig := apiGwProvider.newApiGwConfig(region)
	apiGwClient, err := apigw.NewClient(apiGwConfig)
	if err != nil {
		gologger.Debug().Msgf("%s endpoint NewClient err: %s", *apiGwClient.Endpoint, err)
		return nil, err
	}
	// apigateway regions
	apiGwRegions, err := apiGwClient.DescribeRegions(&apigw.DescribeRegionsRequest{})
	if err != nil {
		return nil, err
	}
	gologger.Debug().Msg("阿里云 ApiGateway 区域信息获取成功")

	// oss client
	ossClient, err := oss.New(fmt.Sprintf("oss-%s.aliyuncs.com", region), accessKeyID, accessKeySecret)
	if err != nil {
		return nil, err
	}
	if okST {
		ossClient.Config.SecurityToken = sessionToken
	}
	gologger.Debug().Msg("阿里云 OSS 客户端创建成功")

	// clb client
	clbProvider := classicLoadBalancerProvider{id: id, provider: utils.Aliyun, config: config}
	clbConfig := clbProvider.newSlbConfig(region)
	clbClient, err := slb.NewClient(clbConfig)
	if err != nil {
		gologger.Debug().Msgf("%s endpoint NewClient err: %s", *clbClient.Endpoint, err)
		return nil, err
	}
	// clb regions
	clbRegions, err := clbClient.DescribeRegions(&slb.DescribeRegionsRequest{})
	if err != nil {
		return nil, err
	}
	gologger.Debug().Msg("阿里云 CLB 区域信息获取成功")

	// ecs client
	ecsConfig := sdk.NewConfig()
	if okST {
		credential := credentials.NewStsTokenCredential(accessKeyID, accessKeySecret, sessionToken)
		ecsClient, err = ecs.NewClientWithOptions(region, ecsConfig, credential)
		if err != nil {
			return nil, err
		}
	} else {
		credential := credentials.NewAccessKeyCredential(accessKeyID, accessKeySecret)
		ecsClient, err = ecs.NewClientWithOptions(region, ecsConfig, credential)
		if err != nil {
			return nil, err
		}
	}
	gologger.Debug().Msg("阿里云 ECS 客户端创建成功")
	// ecs regions
	ecsRegions, err := ecsClient.DescribeRegions(ecs.CreateDescribeRegionsRequest())
	if err != nil {
		return nil, err
	}
	gologger.Debug().Msg("阿里云 ECS 区域信息获取成功")

	// es client
	esProvider := elasticSearchProvider{id: id, provider: utils.Aliyun, config: config}
	esConfig := esProvider.newEsConfig(region)
	esClient, err := es.NewClient(esConfig)
	if err != nil {
		gologger.Debug().Msgf("%s endpoint NewClient err: %s", *esConfig.Endpoint, err)
		return nil, err
	}
	// es regions
	esRegions, err := esClient.DescribeRegions()
	if err != nil {
		return nil, err
	}
	gologger.Debug().Msg("阿里云 ES 区域信息获取成功")

	// rds client
	rdsConfig := sdk.NewConfig()
	if okST {
		credential := credentials.NewStsTokenCredential(accessKeyID, accessKeySecret, sessionToken)
		rdsClient, err = rds.NewClientWithOptions(region, rdsConfig, credential)
		if err != nil {
			return nil, err
		}
	} else {
		credential := credentials.NewAccessKeyCredential(accessKeyID, accessKeySecret)
		rdsClient, err = rds.NewClientWithOptions(region, rdsConfig, credential)
		if err != nil {
			return nil, err
		}
	}
	gologger.Debug().Msg("阿里云 RDS 客户端创建成功")

	//rds regions
	rdsRegions, err := rdsClient.DescribeRegions(rds.CreateDescribeRegionsRequest())
	if err != nil {
		return nil, err
	}
	gologger.Debug().Msg("阿里云 RDS 区域信息获取成功")

	// fc regions
	fcRegions, err := GetFcRegions()
	if err != nil {
		return nil, err
	}
	gologger.Debug().Msgf("阿里云 FC 区域信息获取成功, 共 %d 个\n", len(fcRegions))

	return &Provider{
		provider: utils.Aliyun, id: id, config: config, identity: identity, apiGwRegions: apiGwRegions,
		ossClient: ossClient, clbRegions: clbRegions, ecsRegions: ecsRegions, esRegions: esRegions,
		rdsRegions: rdsRegions, fcRegions: fcRegions,
	}, nil
}

func (p *Provider) shouldRun(t string) bool {
	if _, ok := p.config.includeService[utils.AllService]; ok {
		return true
	}

	lt := strings.ToLower(t)
	if _, ok := p.config.includeService[lt]; ok || lt == utils.AllService {
		return true
	}

	return false
}

func (p *Provider) Resources(ctx context.Context) (*schema.Resources, error) {
	var err error

	if p.shouldRun(utils.AliyunApiGW) {
		apiGwProv := &apiGatewayProvider{id: p.id, provider: p.provider, config: p.config, apiGWRegions: p.apiGwRegions}
		apiGwList, err = apiGwProv.GetResource()
		gologger.Info().Msgf("获取到 %d 条阿里云 ApiGateway 信息", len(apiGwList.GetItems()))
		if err != nil {
			return nil, err
		}
	}

	if p.shouldRun(utils.AliyunCDN) {
		cdnProv := &cdnProvider{id: p.id, provider: p.provider, config: p.config}
		cdnList, err = cdnProv.GetResource()
		gologger.Info().Msgf("获取到 %d 条阿里云 CDN 信息", len(cdnList.GetItems()))
		if err != nil {
			return nil, err
		}
	}

	if p.shouldRun(utils.AliyunDCDN) {
		dcdnProv := &dcdnProvider{id: p.id, provider: p.provider, config: p.config}
		dcdnList, err = dcdnProv.GetResource()
		gologger.Info().Msgf("获取到 %d 条阿里云 DCDN 信息", len(dcdnList.GetItems()))
		if err != nil {
			return nil, err
		}
	}

	// 非 EIP, 只获取传统负载均衡 (clb) 固定公网 ip, eip 应在 eip 获取
	if p.shouldRun(utils.AliyunCLB) {
		clbProv := &classicLoadBalancerProvider{id: p.id, provider: p.provider, clbRegions: p.clbRegions, config: p.config}
		clbList, err = clbProv.GetResource()
		gologger.Info().Msgf("获取到 %d 条阿里云 CLB 信息", len(clbList.GetItems()))
		if err != nil {
			return nil, err
		}
	}

	if p.shouldRun(utils.AliyunECS) {
		ecsProvider := &instanceProvider{id: p.id, provider: p.provider, ecsRegions: p.ecsRegions, config: p.config}
		ecsList, err = ecsProvider.GetEcsResource(ctx)
		gologger.Info().Msgf("获取到 %d 条阿里云 ECS 信息", len(ecsList.GetItems()))
		if err != nil {
			return nil, err
		}
	}

	if p.shouldRun(utils.AliyunES) {
		esProvider := &elasticSearchProvider{id: p.id, provider: p.provider, esRegions: p.esRegions, config: p.config}
		esList, err = esProvider.GetEsResource(ctx)
		gologger.Info().Msgf("获取到 %d 条阿里云 ES 信息", len(esList.GetItems()))
		if err != nil {
			return nil, err
		}
	}

	if p.shouldRun(utils.AliyunFC) {
		fcProvider := functionProvider{
			id: p.id, provider: p.provider, config: p.config,
			fcRegions: p.fcRegions, identity: p.identity,
		}
		fcList, err = fcProvider.GetResource()
		if err != nil {
			return nil, err
		}
		gologger.Info().Msgf("获取到 %d 条阿里云 FC 信息", len(fcList.GetItems()))
	}

	if p.shouldRun(utils.AliyunRDS) {
		rdsProvider := &dbInstanceProvider{id: p.id, provider: p.provider, rdsRegions: p.rdsRegions, config: p.config}
		rdsList, err = rdsProvider.GetRdsResource(ctx)
		if err != nil {
			return nil, err
		}
		gologger.Info().Msgf("获取到 %d 条阿里云 RDS 信息", len(rdsList.GetItems()))
	}

	if p.shouldRun(utils.AliyunOSS) {
		ossProvider := &ossBucketProvider{ossClient: p.ossClient, id: p.id, provider: p.provider}
		bucketList, err = ossProvider.GetResource(ctx)
		if err != nil {
			return nil, err
		}
		gologger.Info().Msgf("获取到 %d 条阿里云 OSS 信息", len(bucketList.GetItems()))
	}

	finalList := schema.NewResources()
	finalList.Merge(apiGwList)
	finalList.Merge(cdnList)
	finalList.Merge(dcdnList)
	finalList.Merge(clbList)
	finalList.Merge(ecsList)
	finalList.Merge(esList)
	finalList.Merge(fcList)
	finalList.Merge(rdsList)
	finalList.Merge(bucketList)
	return finalList, nil
}

func (p *Provider) Name() string {
	return p.provider
}
func (p *Provider) ID() string {
	return p.id
}
