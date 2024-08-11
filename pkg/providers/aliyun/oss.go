package aliyun

import (
	"context"
	"github.com/aliyun/aliyun-oss-go-sdk/oss"
	"github.com/projectdiscovery/gologger"
	"github.com/wgpsec/lc/pkg/schema"
	"strings"
)

type ossBucketProvider struct {
	id        string
	provider  string
	ossClient *oss.Client
}

var bucketList = schema.NewResources()

func (d *ossBucketProvider) GetResource(ctx context.Context) (*schema.Resources, error) {
	marker := oss.Marker("")
	gologger.Debug().Msg("正在获取阿里云 OSS 资源信息")
	for {
		response, err := d.ossClient.ListBuckets(oss.MaxKeys(1000), marker)
		if err != nil {
			break
		}
		marker = oss.Marker(response.NextMarker)
		for _, bucket := range response.Buckets {
			endpointBuilder := &strings.Builder{}
			endpointBuilder.WriteString(bucket.Name)
			endpointBuilder.WriteString(".oss-" + bucket.Region)
			endpointBuilder.WriteString(".aliyuncs.com")
			bucketList.Append(&schema.Resource{
				ID:       d.id,
				Public:   true,
				DNSName:  endpointBuilder.String(),
				Provider: d.provider,
			})
		}
		if !response.IsTruncated {
			break
		}
	}
	return bucketList, nil
}
