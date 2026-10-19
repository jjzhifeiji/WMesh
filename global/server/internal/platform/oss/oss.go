// Package oss 封装 S3 兼容对象存储：保证桶在，并按键存取软件包字节。
// 不决定谁能存什么；调用方先过应用服务。
package oss

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/smithy-go"

	"wmesh/global/internal/platform/blob"
)

// Client 绑定一个端点和一个默认桶；RustFS / SeaweedFS / MinIO 等 S3 兼容实现都能接。
type Client struct {
	s3     *s3.Client // 路径风格的对象存储客户端
	bucket string     // 本侧默认桶名
}

// New 用静态密钥和路径风格地址组装客户端；私有部署没有虚拟主机域名，只能走路径风格。
func New(endpoint, bucket, accessKey, secretKey string) *Client {
	// 用静态密钥组客户端，地址走路径风格。
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
	// 探桶最多等五秒，避免启动挂死。
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	// 探完就取消，含提前返回。
	defer cancel()
	// 先看默认桶在不在。
	_, err := c.s3.HeadBucket(ctx, &s3.HeadBucketInput{Bucket: aws.String(c.bucket)})
	// 已经在就不用再建。
	if err == nil {
		return nil
	}
	// 准备接「没有这个桶」。
	var notFound *types.NotFound
	// 不是找不到就当存储故障。
	if !errors.As(err, &notFound) {
		// 带上桶名交回，方便对端点。
		return fmt.Errorf("oss head bucket %q: %w", c.bucket, err)
	}
	// 没有就建；别人同时建上也算成功。
	if _, err := c.s3.CreateBucket(ctx, &s3.CreateBucketInput{Bucket: aws.String(c.bucket)}); err != nil {
		// 准备接「桶已属于自己」。
		var owned *types.BucketAlreadyOwnedByYou
		// 准备接「桶已经存在」。
		var exists *types.BucketAlreadyExists
		// 并发建桶被判已有，也当成功。
		if errors.As(err, &owned) || errors.As(err, &exists) {
			return nil
		}
		// 别的失败带上桶名交回。
		return fmt.Errorf("oss create bucket %q: %w", c.bucket, err)
	}
	return nil
}

// EnsureBucketRetry 给启动阶段用：对象存储可能比应用晚就绪，按间隔重试几次再放弃。
func (c *Client) EnsureBucketRetry(ctx context.Context, attempts int, wait time.Duration) error {
	// 留下最后一次失败，重试尽了再交回。
	var err error
	// 按次数试，成功一次就停。
	for i := 0; i < attempts; i++ {
		// 这一次成功就不用再等。
		if err = c.EnsureBucket(ctx); err == nil {
			return nil
		}
		// 要么调用方取消，要么等到下一轮。
		select {
		// 调用方取消就停下，不再空等。
		case <-ctx.Done():
			// 把调用方取消的原因交回。
			return ctx.Err()
		// 间隔到了再试下一次。
		case <-time.After(wait):
		}
	}
	return err
}

// 对象不存在时统一成 blob 找不到，避免把 S3 原文抛给上层。
func isMissingObject(err error) bool {
	// 准备接「没有这个键」。
	var nsk *types.NoSuchKey
	// 键不存在是一种找不到。
	if errors.As(err, &nsk) {
		return true
	}
	// 准备接「对象不存在」。
	var nf *types.NotFound
	// 类型对上也不当存储故障。
	if errors.As(err, &nf) {
		return true
	}
	// 有的实现只给错误码，不给类型。
	var ae smithy.APIError
	// 能收成接口错误才看错误码。
	if errors.As(err, &ae) {
		// 这几个码都当对象不存在。
		switch ae.ErrorCode() {
		// 键、对象或资源不在，都当找不到。
		case "NoSuchKey", "NotFound", "NoSuchObject":
			return true
		}
	}
	return false
}

// Put 按键覆盖写入软件包字节。
func (c *Client) Put(ctx context.Context, key string, body []byte) error {
	// 按键覆盖写入这一段字节。
	_, err := c.s3.PutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String(c.bucket),
		Key:    aws.String(key),
		Body:   bytes.NewReader(body),
	})
	// 写失败则带上键交回。
	if err != nil {
		// 包上键名，上层不用看存储原文。
		return fmt.Errorf("oss put %q: %w", key, err)
	}
	return nil
}

// Get 按键读出软件包字节；没有该键则找不到。
func (c *Client) Get(ctx context.Context, key string) ([]byte, error) {
	// 按这个键把对象取出来。
	out, err := c.s3.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(c.bucket),
		Key:    aws.String(key),
	})
	// 取失败要区分没有该键和存储故障。
	if err != nil {
		// 没有该键收成统一的找不到。
		if isMissingObject(err) {
			return nil, blob.ErrNotFound
		}
		// 别的失败带上键名交回。
		return nil, fmt.Errorf("oss get %q: %w", key, err)
	}
	// 读完就关上正文，避免占着连接。
	defer out.Body.Close()
	// 把对象正文一次读完。
	body, err := io.ReadAll(out.Body)
	// 读一半失败则带上键交回。
	if err != nil {
		// 包上键名，上层不用看存储原文。
		return nil, fmt.Errorf("oss read %q: %w", key, err)
	}
	return body, nil
}

// Open 按键打开只读流：先问总长，读时才按当前偏移带 Range 去取。
func (c *Client) Open(ctx context.Context, key string) (blob.Object, error) {
	// 先问对象头，拿到总长，不取正文。
	head, err := c.s3.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(c.bucket),
		Key:    aws.String(key),
	})
	// 问不到要区分没有该键和存储故障。
	if err != nil {
		// 没有该键收成统一的找不到。
		if isMissingObject(err) {
			return nil, blob.ErrNotFound
		}
		// 别的失败带上键名交回。
		return nil, fmt.Errorf("oss head %q: %w", key, err)
	}
	// 记下键和总长，正文等第一次读再取。
	return &object{c: c, ctx: ctx, key: key, size: aws.ToInt64(head.ContentLength)}, nil
}

// object 是按偏移分段取的对象流；定位只记位置，读时才开正文。
type object struct {
	c    *Client         // 发请求用的客户端和桶
	ctx  context.Context // 打开时的上下文，读正文跟着它取消
	key  string          // 对象键，取正文和报错都用它
	size int64           // 对象总长，打开时由对象头给出
	off  int64           // 下一次读的起点
	body io.ReadCloser   // 从 off 起的正文；定位后作废，读时再开
}

// Size 交回打开时问到的对象总长。
func (o *object) Size() int64 { return o.size }

// Read 从当前偏移往后读；还没开正文就带 Range 去取。
func (o *object) Read(p []byte) (int, error) {
	// 已读到末尾就交回结束，不再去取。
	if o.off >= o.size {
		return 0, io.EOF
	}
	// 还没开正文就从当前偏移起取到末尾。
	if o.body == nil {
		// 带上起点去取，续读不必从头下。
		out, err := o.c.s3.GetObject(o.ctx, &s3.GetObjectInput{
			Bucket: aws.String(o.c.bucket),
			Key:    aws.String(o.key),
			Range:  aws.String("bytes=" + strconv.FormatInt(o.off, 10) + "-"),
		})
		// 取不到要区分没有该键和存储故障。
		if err != nil {
			// 没有该键收成统一的找不到。
			if isMissingObject(err) {
				return 0, blob.ErrNotFound
			}
			// 别的失败带上键名交回。
			return 0, fmt.Errorf("oss get %q: %w", o.key, err)
		}
		// 记下这段正文，后面接着读。
		o.body = out.Body
	}
	// 从正文里读一段，读多少偏移就往后挪多少。
	n, err := o.body.Read(p)
	// 偏移跟着读到的字节往后挪。
	o.off += int64(n)
	// 正文读完就关上，下一次读按偏移判断。
	if errors.Is(err, io.EOF) {
		// 关上这段正文，连接还给池子。
		_ = o.body.Close()
		// 清掉正文，需要时再按偏移重开。
		o.body = nil
		// 没读够总长就是中途断了，不当读完。
		if o.off < o.size {
			return n, io.ErrUnexpectedEOF
		}
		return n, io.EOF
	}
	// 读一半出错就带上键名交回。
	if err != nil {
		// 包上键名，上层不用看存储原文。
		return n, fmt.Errorf("oss read %q: %w", o.key, err)
	}
	return n, nil
}

// Seek 只挪偏移；位置变了就作废已开的正文，下次读再按新位置取。
func (o *object) Seek(offset int64, whence int) (int64, error) {
	// 预备算出新的绝对位置。
	var abs int64
	// 按起算点换成绝对位置。
	switch whence {
	// 从头算起。
	case io.SeekStart:
		// 偏移本身就是绝对位置。
		abs = offset
	// 从当前位置算起。
	case io.SeekCurrent:
		// 当前位置加上偏移。
		abs = o.off + offset
	// 从末尾算起。
	case io.SeekEnd:
		// 总长加上偏移。
		abs = o.size + offset
	// 不认识的起算点直接拒绝。
	default:
		// 交回起算点不认识，不挪偏移。
		return 0, errors.New("oss seek: invalid whence")
	}
	// 落到开头之前不合法，拒绝。
	if abs < 0 {
		// 交回位置不合法，不挪偏移。
		return 0, errors.New("oss seek: negative position")
	}
	// 位置变了就关掉旧正文，免得接着读错位。
	if abs != o.off && o.body != nil {
		// 关上旧正文，连接还给池子。
		_ = o.body.Close()
		// 清掉旧正文，下次读按新位置重开。
		o.body = nil
	}
	// 记下新位置。
	o.off = abs
	return abs, nil
}

// Close 关掉已开的正文；没开过就什么也不做。
func (o *object) Close() error {
	// 没开过正文就不用关。
	if o.body == nil {
		return nil
	}
	// 关上正文，连接还给池子。
	err := o.body.Close()
	// 清掉正文，重复关也不出错。
	o.body = nil
	return err
}

// Delete 按键删掉软件包字节；没有该键也算成功。
func (c *Client) Delete(ctx context.Context, key string) error {
	// 按键删掉对象，可重复删。
	_, err := c.s3.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(c.bucket),
		Key:    aws.String(key),
	})
	// 删失败要区分没有该键和存储故障。
	if err != nil {
		// 没有该键也当成功，删除可重复。
		if isMissingObject(err) {
			return nil
		}
		// 别的失败带上键名交回。
		return fmt.Errorf("oss delete %q: %w", key, err)
	}
	return nil
}

// Usage 按桶列出对象合计已用字节；没有配额，不算盘余量。
func (c *Client) Usage(ctx context.Context) (used, objects int64, err error) {
	// 分页记号，第一页为空。
	var token *string
	// 按页列完整个桶再合计。
	for {
		// 按页记号再取下一页。
		out, err := c.s3.ListObjectsV2(ctx, &s3.ListObjectsV2Input{
			Bucket:            aws.String(c.bucket),
			ContinuationToken: token,
		})
		// 列失败则字节和份数都不可信。
		if err != nil {
			// 带上桶名交回，不报半套数字。
			return 0, 0, fmt.Errorf("oss list %q: %w", c.bucket, err)
		}
		// 这一页的对象都计入字节和份数。
		for _, obj := range out.Contents {
			// 这一份计入对象数。
			objects++
			// 这一份的大小累进已用。
			used += aws.ToInt64(obj.Size)
		}
		// 没有下一页就把合计交回。
		if !aws.ToBool(out.IsTruncated) {
			return used, objects, nil
		}
		// 记下页记号，下一轮接着列。
		token = out.NextContinuationToken
	}
}
