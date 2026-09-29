// Package oss 封装 S3 兼容对象存储：保证桶在，并按键存取软件包字节。
// 不决定谁能存什么；调用方先过应用服务。
package oss

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/smithy-go"

	"wmesh/factory/internal/platform/blob"
)

// Client 绑定一个端点和一个默认桶；RustFS / SeaweedFS / MinIO 等 S3 兼容实现都能接。
type Client struct {
	s3     *s3.Client // 访问兼容对象存储的客户端。
	bucket string     // 本厂默认桶的名字，对象都放这里。
}

// New 用静态密钥和路径风格地址组装客户端；私有部署没有虚拟主机域名，只能走路径风格。
func New(endpoint, bucket, accessKey, secretKey string) *Client {
	// 按路径风格连上对象存储。
	cli := s3.New(s3.Options{
		BaseEndpoint: aws.String(endpoint),
		Region:       "us-east-1",
		Credentials:  credentials.NewStaticCredentialsProvider(accessKey, secretKey, ""),
		UsePathStyle: true,
	})
	return &Client{s3: cli, bucket: bucket}
}

// Bucket 是本侧默认桶名。
func (c *Client) Bucket() string { return c.bucket }

// EnsureBucket 幂等地保证默认桶存在：已有就直接返回，没有就建；并发建桶被判已存在也算成功。
func (c *Client) EnsureBucket(ctx context.Context) error {
	// 给上下文一段相对限时。
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	// 用完就取消限时，避免上下文一直占着。
	defer cancel()
	// 收成对象存储要求的字符串。
	_, err := c.s3.HeadBucket(ctx, &s3.HeadBucketInput{Bucket: aws.String(c.bucket)})
	// 没有出错就按成功返回，不用再补救。
	if err == nil {
		return nil
	}
	// 准备承接对象不存在这种错误。
	var notFound *types.NotFound
	// 不满足就停住或跳过，避免做错下一步。
	if !errors.As(err, &notFound) {
		// 探桶失败，带上桶名交回。
		return fmt.Errorf("oss head bucket %q: %w", c.bucket, err)
	}
	// 没能收成对象存储要求的字符串就停，避免带着残缺继续。
	if _, err := c.s3.CreateBucket(ctx, &s3.CreateBucketInput{Bucket: aws.String(c.bucket)}); err != nil {
		// 承接桶已属于自己，并发创建不算失败。
		var owned *types.BucketAlreadyOwnedByYou
		// 承接桶已经存在，并发创建不算失败。
		var exists *types.BucketAlreadyExists
		if errors.As(err, &owned) || errors.As(err, &exists) {
			return nil
		}
		return fmt.Errorf("oss create bucket %q: %w", c.bucket, err)
	}
	return nil
}

// EnsureBucketRetry 给启动阶段用：对象存储可能比应用晚就绪，按间隔重试几次再放弃。
func (c *Client) EnsureBucketRetry(ctx context.Context, attempts int, wait time.Duration) error {
	// 先记下失败，重试完再决定交不交出去。
	var err error
	// 按下标扫过去，直到越界或提前结束。
	for i := 0; i < attempts; i++ {
		// 没有出错就按成功返回，不用再补救。
		if err = c.EnsureBucket(ctx); err == nil {
			return nil
		}
		// 等取消或时间到，先发生的那一路先处理。
		select {
		// 调用方取消了就停下，不再空等。
		case <-ctx.Done():
			// 交回取出取消的原因的结果。
			return ctx.Err()
		// 间隔到了再试一次，给对方一点时间。
		case <-time.After(wait):
		}
	}
	return err
}

// 对象不存在时统一成 blob 找不到，避免把 S3 原文抛给上层。
func isMissingObject(err error) bool {
	// 先把这一步的结果放下，后面还要用。
	var nsk *types.NoSuchKey
	// 没能收成具体那一种错误就停，避免带着残缺继续。
	if errors.As(err, &nsk) {
		return true
	}
	// 先把这一步的结果放下，后面还要用。
	var nf *types.NotFound
	// 没能收成具体那一种错误就停，避免带着残缺继续。
	if errors.As(err, &nf) {
		return true
	}
	// 先把这一步的结果放下，后面还要用。
	var ae smithy.APIError
	// 没能收成具体那一种错误就停，避免带着残缺继续。
	if errors.As(err, &ae) {
		// 按存储返回的错误码区分不存在和别的失败。
		switch ae.ErrorCode() {
		// 这几种错误码都表示对象不存在。
		case "NoSuchKey", "NotFound", "NoSuchObject":
			return true
		}
	}
	return false
}

// Put 按键覆盖写入软件包字节。
func (c *Client) Put(ctx context.Context, key string, body []byte) error {
	// 按键把字节写进桶里。
	_, err := c.s3.PutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String(c.bucket),
		Key:    aws.String(key),
		Body:   bytes.NewReader(body),
	})
	// 没能把内存里的字节包成可读流就停，避免带着残缺继续。
	if err != nil {
		// 上传失败，带上键交回。
		return fmt.Errorf("oss put %q: %w", key, err)
	}
	return nil
}

// Get 按键读出软件包字节；没有该键则找不到。
func (c *Client) Get(ctx context.Context, key string) ([]byte, error) {
	// 按键把字节从桶里读出。
	out, err := c.s3.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(c.bucket),
		Key:    aws.String(key),
	})
	// 没能收成普通文本再拿去比较或拼接就停，避免带着残缺继续。
	if err != nil {
		// 这是对象不存在，收成找不到而不是别的故障。
		if isMissingObject(err) {
			return nil, blob.ErrNotFound
		}
		// 下载失败，带上键交回。
		return nil, fmt.Errorf("oss get %q: %w", key, err)
	}
	// 离开时关掉，避免句柄或连接漏掉。
	defer out.Body.Close()
	// 把流里的字节全部读完。
	body, err := io.ReadAll(out.Body)
	// 没能把流里的字节全部读完就停，避免带着残缺继续。
	if err != nil {
		// 脚本读不出来，带上文件名交回。
		return nil, fmt.Errorf("oss read %q: %w", key, err)
	}
	return body, nil
}

// Delete 按键删掉软件包字节；没有该键也算成功。
func (c *Client) Delete(ctx context.Context, key string) error {
	// 按键把桶里的对象删掉。
	_, err := c.s3.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(c.bucket),
		Key:    aws.String(key),
	})
	// 没能收成普通文本再拿去比较或拼接就停，避免带着残缺继续。
	if err != nil {
		// 这是对象不存在，收成找不到而不是别的故障。
		if isMissingObject(err) {
			return nil
		}
		// 删除失败，带上键交回。
		return fmt.Errorf("oss delete %q: %w", key, err)
	}
	return nil
}

// Usage 按桶列出对象合计已用字节；没有配额，不算盘余量。
func (c *Client) Usage(ctx context.Context) (used, objects int64, err error) {
	// 准备放下令牌，再交给后面。
	var token *string
	// 一直看下去，直到就绪、失败或被取消。
	for {
		// 列出取值，再交给后面。
		out, err := c.s3.ListObjectsV2(ctx, &s3.ListObjectsV2Input{
			Bucket:            aws.String(c.bucket),
			ContinuationToken: token,
		})
		// 这一步没做成就停，避免带着残缺继续。
		if err != nil {
			// 列出对象失败，带上桶名交回。
			return 0, 0, fmt.Errorf("oss list %q: %w", c.bucket, err)
		}
		// 逐项处理，空的就不进入循环。
		for _, obj := range out.Contents {
			// 把这个值定下来，后面的判断才有依据。
			objects++
			// 成整数，再交给后面。
			used += aws.ToInt64(obj.Size)
		}
		// 不满足就停住或跳过，避免做错下一步。
		if !aws.ToBool(out.IsTruncated) {
			return used, objects, nil
		}
		// 定下令牌，再交给后面。
		token = out.NextContinuationToken
	}
}
