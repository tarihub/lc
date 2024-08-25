package aliyun

import cloudapi "github.com/alibabacloud-go/cloudapi-20160714/v5/client"

type apiGatewayProvider struct {
	id           string
	provider     string
	config       providerConfig
	apiGWRegions *cloudapi.DescribeRegionsResponse
}
