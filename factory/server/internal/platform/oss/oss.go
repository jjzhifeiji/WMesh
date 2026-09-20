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
	s3     *s3.Client
	bucket string
}

// New 用静态密钥和路径风格地址组装客户端；私有部署没有虚拟主机域名，只能走路径风格。
func New(endpoint, bucket, accessKey, secretKey string) *Client {
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
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	_, err := c.s3.HeadBucket(ctx, &s3.HeadBucketInput{Bucket: aws.String(c.bucket)})
	if err == nil {
		return nil
	}
	var notFound *types.NotFound
	if !errors.As(err, &notFound) {
		return fmt.Errorf("oss head bucket %q: %w", c.bucket, err)
	}
	if _, err := c.s3.CreateBucket(ctx, &s3.CreateBucketInput{Bucket: aws.String(c.bucket)}); err != nil {
		var owned *types.BucketAlreadyOwnedByYou
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
	var err error
	for i := 0; i < attempts; i++ {
		if err = c.EnsureBucket(ctx); err == nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(wait):
		}
	}
	return err
}

// 对象不存在时统一成 blob 找不到，避免把 S3 原文抛给上层。
func isMissingObject(err error) bool {
	var nsk *types.NoSuchKey
	if errors.As(err, &nsk) {
		return true
	}
	var nf *types.NotFound
	if errors.As(err, &nf) {
		return true
	}
	var ae smithy.APIError
	if errors.As(err, &ae) {
		switch ae.ErrorCode() {
		case "NoSuchKey", "NotFound", "NoSuchObject":
			return true
		}
	}
	return false
}

// Put 按键覆盖写入软件包字节。
func (c *Client) Put(ctx context.Context, key string, body []byte) error {
	_, err := c.s3.PutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String(c.bucket),
		Key:    aws.String(key),
		Body:   bytes.NewReader(body),
	})
	if err != nil {
		return fmt.Errorf("oss put %q: %w", key, err)
	}
	return nil
}

// Get 按键读出软件包字节；没有该键则找不到。
func (c *Client) Get(ctx context.Context, key string) ([]byte, error) {
	out, err := c.s3.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(c.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		if isMissingObject(err) {
			return nil, blob.ErrNotFound
		}
		return nil, fmt.Errorf("oss get %q: %w", key, err)
	}
	defer out.Body.Close()
	body, err := io.ReadAll(out.Body)
	if err != nil {
		return nil, fmt.Errorf("oss read %q: %w", key, err)
	}
	return body, nil
}

// Delete 按键删掉软件包字节；没有该键也算成功。
func (c *Client) Delete(ctx context.Context, key string) error {
	_, err := c.s3.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(c.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		if isMissingObject(err) {
			return nil
		}
		return fmt.Errorf("oss delete %q: %w", key, err)
	}
	return nil
}

// Usage 按桶列出对象合计已用字节；没有配额，不算盘余量。
func (c *Client) Usage(ctx context.Context) (used, objects int64, err error) {
	var token *string
	for {
		out, err := c.s3.ListObjectsV2(ctx, &s3.ListObjectsV2Input{
			Bucket:            aws.String(c.bucket),
			ContinuationToken: token,
		})
		if err != nil {
			return 0, 0, fmt.Errorf("oss list %q: %w", c.bucket, err)
		}
		for _, obj := range out.Contents {
			objects++
			used += aws.ToInt64(obj.Size)
		}
		if !aws.ToBool(out.IsTruncated) {
			return used, objects, nil
		}
		token = out.NextContinuationToken
	}
}
