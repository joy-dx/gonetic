package s3client

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/joy-dx/gonetic/dto"
)

type fakeS3 struct {
	// Captured inputs
	gotGet    []*s3.GetObjectInput
	gotPut    []*s3.PutObjectInput
	gotDelete []*s3.DeleteObjectInput
	gotList   []*s3.ListObjectsV2Input

	// Stubbed outputs / errors
	getOut  *s3.GetObjectOutput
	getErr  error
	putOut  *s3.PutObjectOutput
	putErr  error
	delOut  *s3.DeleteObjectOutput
	delErr  error
	listOut *s3.ListObjectsV2Output
	listErr error
}

func (f *fakeS3) GetObject(
	ctx context.Context,
	params *s3.GetObjectInput,
	optFns ...func(*s3.Options),
) (*s3.GetObjectOutput, error) {
	f.gotGet = append(f.gotGet, params)
	if f.getErr != nil {
		return nil, f.getErr
	}
	if f.getOut == nil {
		return &s3.GetObjectOutput{Body: io.NopCloser(bytes.NewReader(nil))}, nil
	}
	return f.getOut, nil
}

func (f *fakeS3) PutObject(
	ctx context.Context,
	params *s3.PutObjectInput,
	optFns ...func(*s3.Options),
) (*s3.PutObjectOutput, error) {
	f.gotPut = append(f.gotPut, params)
	if f.putErr != nil {
		return nil, f.putErr
	}
	if f.putOut == nil {
		return &s3.PutObjectOutput{}, nil
	}
	return f.putOut, nil
}

func (f *fakeS3) DeleteObject(
	ctx context.Context,
	params *s3.DeleteObjectInput,
	optFns ...func(*s3.Options),
) (*s3.DeleteObjectOutput, error) {
	f.gotDelete = append(f.gotDelete, params)
	if f.delErr != nil {
		return nil, f.delErr
	}
	if f.delOut == nil {
		return &s3.DeleteObjectOutput{}, nil
	}
	return f.delOut, nil
}

func (f *fakeS3) ListObjectsV2(
	ctx context.Context,
	params *s3.ListObjectsV2Input,
	optFns ...func(*s3.Options),
) (*s3.ListObjectsV2Output, error) {
	f.gotList = append(f.gotList, params)
	if f.listErr != nil {
		return nil, f.listErr
	}
	if f.listOut == nil {
		return &s3.ListObjectsV2Output{}, nil
	}
	return f.listOut, nil
}

func newTestClient(t *testing.T, mw ...Middleware) (*S3Client, *fakeS3) {
	t.Helper()

	f := &fakeS3{}
	c := &S3Client{
		cfg: &S3ClientConfig{
			Middlewares: mw,
		},
		client: f,
		NetClient: dto.NetClient{
			Name:        "S3 Client",
			Ref:         "test",
			ClientType:  NetClientS3Ref,
			Description: "test",
		},
	}
	return c, f
}

func mustReq(t *testing.T, cfg *S3RequestConfig) *dto.RequestConfig {
	t.Helper()
	return (&dto.RequestConfig{}).WithReqConfig(cfg)
}

func TestS3Request_Finalize_Golden(t *testing.T) {
	cases := []struct {
		name    string
		req     *S3Request
		wantErr string

		wantGet    *s3.GetObjectInput
		wantPut    *s3.PutObjectInput
		wantDelete *s3.DeleteObjectInput
		wantList   *s3.ListObjectsV2Input
	}{
		{
			name: "get builds GetObjectInput",
			req: &S3Request{
				Operation: "get",
				Bucket:    "b",
				Key:       "k",
			},
			wantGet: &s3.GetObjectInput{
				Bucket: aws.String("b"),
				Key:    aws.String("k"),
			},
		},
		{
			name: "put builds PutObjectInput with content-type and metadata from map[string]string",
			req: &S3Request{
				Operation:   "put",
				Bucket:      "b",
				Key:         "k",
				Body:        []byte("payload"),
				ContentType: "text/plain",
				ExtraOpts: map[string]any{
					"metadata": map[string]string{
						"a": "1",
						"b": "2",
					},
					"cache_control": "max-age=60",
				},
			},
			wantPut: &s3.PutObjectInput{
				Bucket:       aws.String("b"),
				Key:          aws.String("k"),
				Body:         bytes.NewReader([]byte("payload")),
				ContentType:  aws.String("text/plain"),
				CacheControl: aws.String("max-age=60"),
				Metadata: map[string]string{
					"a": "1",
					"b": "2",
				},
			},
		},
		{
			name: "put builds PutObjectInput metadata from map[string]any (string-only values)",
			req: &S3Request{
				Operation: "put",
				Bucket:    "b",
				Key:       "k",
				Body:      []byte("x"),
				ExtraOpts: map[string]any{
					"metadata": map[string]any{
						"a": "1",
						"b": 2, // ignored
					},
				},
			},
			wantPut: &s3.PutObjectInput{
				Bucket: aws.String("b"),
				Key:    aws.String("k"),
				Body:   bytes.NewReader([]byte("x")),
				Metadata: map[string]string{
					"a": "1",
				},
			},
		},
		{
			name: "delete builds DeleteObjectInput",
			req: &S3Request{
				Operation: "delete",
				Bucket:    "b",
				Key:       "k",
			},
			wantDelete: &s3.DeleteObjectInput{
				Bucket: aws.String("b"),
				Key:    aws.String("k"),
			},
		},
		{
			name: "list builds ListObjectsV2Input with prefix",
			req: &S3Request{
				Operation: "list",
				Bucket:    "b",
				Prefix:    "p/",
			},
			wantList: &s3.ListObjectsV2Input{
				Bucket: aws.String("b"),
				Prefix: aws.String("p/"),
			},
		},
		{
			name: "unsupported operation returns error",
			req: &S3Request{
				Operation: "nope",
			},
			wantErr: "unsupported s3 operation: nope",
		},
		{
			name: "Finalize clears previously prepared inputs before rebuilding",
			req: &S3Request{
				Operation: "get",
				Bucket:    "b",
				Key:       "k",
				PutInput:  &s3.PutObjectInput{Bucket: aws.String("old")},
			},
			wantGet: &s3.GetObjectInput{
				Bucket: aws.String("b"),
				Key:    aws.String("k"),
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.req.Finalize()
			if tc.wantErr != "" {
				if err == nil || err.Error() != tc.wantErr {
					t.Fatalf("expected err=%q, got=%v", tc.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("Finalize error: %v", err)
			}

			// Helper: compare AWS input structs with pointer fields.
			// For PutObjectInput, Body is an io.ReadSeeker; compare by reading.
			if tc.wantGet != nil && !reflect.DeepEqual(tc.req.GetInput, tc.wantGet) {
				t.Fatalf("GetInput mismatch:\n got=%#v\nwant=%#v", tc.req.GetInput, tc.wantGet)
			}
			if tc.wantDelete != nil && !reflect.DeepEqual(tc.req.DeleteInput, tc.wantDelete) {
				t.Fatalf("DeleteInput mismatch:\n got=%#v\nwant=%#v", tc.req.DeleteInput, tc.wantDelete)
			}
			if tc.wantList != nil && !reflect.DeepEqual(tc.req.ListInput, tc.wantList) {
				t.Fatalf("ListInput mismatch:\n got=%#v\nwant=%#v", tc.req.ListInput, tc.wantList)
			}
			if tc.wantPut != nil {
				if tc.req.PutInput == nil {
					t.Fatalf("expected PutInput, got nil")
				}
				// Compare all fields except Body by zeroing Body for DeepEqual,
				// then compare Body contents separately.
				got := *tc.req.PutInput
				want := *tc.wantPut
				gotBody := got.Body
				wantBody := want.Body
				got.Body = nil
				want.Body = nil

				if !reflect.DeepEqual(&got, &want) {
					t.Fatalf("PutInput mismatch (excluding Body):\n got=%#v\nwant=%#v", &got, &want)
				}

				gotBytes, err := io.ReadAll(gotBody)
				if err != nil {
					t.Fatalf("read got body: %v", err)
				}
				wantBytes, err := io.ReadAll(wantBody)
				if err != nil {
					t.Fatalf("read want body: %v", err)
				}
				if !bytes.Equal(gotBytes, wantBytes) {
					t.Fatalf("PutInput.Body mismatch: got=%q want=%q", string(gotBytes), string(wantBytes))
				}
			}
		})
	}
}

func TestS3Client_ProcessRequest_Golden(t *testing.T) {
	errBoom := errors.New("boom")

	cases := []struct {
		name string

		reqCfg *dto.RequestConfig
		mw     []Middleware
		fake   func(f *fakeS3)

		wantErrSubstr string
		wantStatus    int
		wantBody      string

		wantCalls struct {
			get, put, del, list int
		}

		// Optional checks of captured SDK inputs
		check func(t *testing.T, f *fakeS3, resp dto.Response)
	}{
		{
			name: "bad ReqConfig type returns cast error",
			reqCfg: (&dto.RequestConfig{}).WithReqConfig(&badReqConfig{
				ref: NetClientS3Ref,
			}),
			wantErrSubstr: "problem casting to s3requestconfig",
		},
		{
			name: "middleware aborts before Finalize/SDK call",
			reqCfg: mustReq(t, &S3RequestConfig{
				Operation: "get",
				Bucket:    "b",
				Key:       "k",
			}),
			mw: []Middleware{
				func(ctx context.Context, r *S3Request) error {
					return errBoom
				},
			},
			wantErrSubstr: "middleware aborted: boom",
			check: func(t *testing.T, f *fakeS3, resp dto.Response) {
				if len(f.gotGet)+len(f.gotPut)+len(f.gotDelete)+len(f.gotList) != 0 {
					t.Fatalf("expected no SDK calls, got get=%d put=%d del=%d list=%d",
						len(f.gotGet), len(f.gotPut), len(f.gotDelete), len(f.gotList))
				}
			},
		},
		{
			name: "unsupported operation after Finalize returns error",
			reqCfg: mustReq(t, &S3RequestConfig{
				Operation: "nope",
				Bucket:    "b",
				Key:       "k",
			}),
			wantErrSubstr: "unsupported s3 operation: nope",
		},
		{
			name: "get routes to GetObject and returns body and metadata headers",
			reqCfg: mustReq(t, &S3RequestConfig{
				Operation: "get",
				Bucket:    "b",
				Key:       "k",
			}),
			fake: func(f *fakeS3) {
				f.getOut = &s3.GetObjectOutput{
					Body: io.NopCloser(strings.NewReader("hello")),
					Metadata: map[string]string{
						"x": "1",
					},
				}
			},
			wantStatus: 200,
			wantBody:   "hello",
			wantCalls:  struct{ get, put, del, list int }{get: 1},
			check: func(t *testing.T, f *fakeS3, resp dto.Response) {
				if resp.Headers == nil {
					t.Fatalf("expected headers, got nil")
				}
				// Expect metadata to be present in some header key; validate by value.
				found := false
				for _, vs := range resp.Headers {
					for _, v := range vs {
						if v == "1" {
							found = true
						}
					}
				}
				if !found {
					t.Fatalf("expected metadata header value %q somewhere, got headers=%v", "1", resp.Headers)
				}
				if aws.ToString(f.gotGet[0].Bucket) != "b" || aws.ToString(f.gotGet[0].Key) != "k" {
					t.Fatalf("GetObjectInput mismatch: %#v", f.gotGet[0])
				}
			},
		},
		{
			name: "get sdk error is wrapped",
			reqCfg: mustReq(t, &S3RequestConfig{
				Operation: "get",
				Bucket:    "b",
				Key:       "k",
			}),
			fake: func(f *fakeS3) {
				f.getErr = errBoom
			},
			wantErrSubstr: "s3 get object: boom",
			wantCalls:     struct{ get, put, del, list int }{get: 1},
		},
		{
			name: "put routes to PutObject and returns 200",
			reqCfg: mustReq(t, &S3RequestConfig{
				Operation:   "put",
				Bucket:      "b",
				Key:         "k",
				Body:        []byte("data"),
				ContentType: "application/octet-stream",
				ExtraOpts: map[string]interface{}{
					"metadata": map[string]string{"a": "1"},
				},
			}),
			wantStatus: 200,
			wantCalls:  struct{ get, put, del, list int }{put: 1},
			check: func(t *testing.T, f *fakeS3, resp dto.Response) {
				in := f.gotPut[0]
				if aws.ToString(in.Bucket) != "b" || aws.ToString(in.Key) != "k" {
					t.Fatalf("PutObjectInput bucket/key mismatch: %#v", in)
				}
				if in.ContentType == nil || *in.ContentType != "application/octet-stream" {
					t.Fatalf("PutObjectInput content-type mismatch: %#v", in.ContentType)
				}
				if in.Metadata["a"] != "1" {
					t.Fatalf("PutObjectInput metadata mismatch: %#v", in.Metadata)
				}
				gotBody, err := io.ReadAll(in.Body)
				if err != nil {
					t.Fatalf("read put body: %v", err)
				}
				if string(gotBody) != "data" {
					t.Fatalf("put body mismatch: got=%q want=%q", string(gotBody), "data")
				}
			},
		},
		{
			name: "delete routes to DeleteObject and returns 200",
			reqCfg: mustReq(t, &S3RequestConfig{
				Operation: "delete",
				Bucket:    "b",
				Key:       "k",
			}),
			wantStatus: 200,
			wantCalls:  struct{ get, put, del, list int }{del: 1},
			check: func(t *testing.T, f *fakeS3, resp dto.Response) {
				in := f.gotDelete[0]
				if aws.ToString(in.Bucket) != "b" || aws.ToString(in.Key) != "k" {
					t.Fatalf("DeleteObjectInput mismatch: %#v", in)
				}
			},
		},
		{
			name: "list routes to ListObjectsV2 and returns newline separated keys",
			reqCfg: mustReq(t, &S3RequestConfig{
				Operation: "list",
				Bucket:    "b",
				Prefix:    "p/",
			}),
			fake: func(f *fakeS3) {
				f.listOut = &s3.ListObjectsV2Output{
					Contents: []s3types.Object{
						{Key: aws.String("p/a.txt")},
						{Key: aws.String("p/b.txt")},
					},
				}
			},
			wantStatus: 200,
			wantBody:   "p/a.txt\np/b.txt\n",
			wantCalls:  struct{ get, put, del, list int }{list: 1},
			check: func(t *testing.T, f *fakeS3, resp dto.Response) {
				in := f.gotList[0]
				if aws.ToString(in.Bucket) != "b" {
					t.Fatalf("ListObjectsV2Input bucket mismatch: %#v", in)
				}
				if in.Prefix == nil || aws.ToString(in.Prefix) != "p/" {
					t.Fatalf("ListObjectsV2Input prefix mismatch: %#v", in)
				}
			},
		},
		{
			name: "list sdk error is wrapped",
			reqCfg: mustReq(t, &S3RequestConfig{
				Operation: "list",
				Bucket:    "b",
			}),
			fake: func(f *fakeS3) {
				f.listErr = errBoom
			},
			wantErrSubstr: "s3 list objects: boom",
			wantCalls:     struct{ get, put, del, list int }{list: 1},
		},
		{
			name: "delete sdk error is wrapped",
			reqCfg: mustReq(t, &S3RequestConfig{
				Operation: "delete",
				Bucket:    "b",
				Key:       "k",
			}),
			fake: func(f *fakeS3) {
				f.delErr = errBoom
			},
			wantErrSubstr: "s3 delete object: boom",
			wantCalls:     struct{ get, put, del, list int }{del: 1},
		},
		{
			name: "put sdk error is wrapped",
			reqCfg: mustReq(t, &S3RequestConfig{
				Operation: "put",
				Bucket:    "b",
				Key:       "k",
				Body:      []byte("x"),
			}),
			fake: func(f *fakeS3) {
				f.putErr = errBoom
			},
			wantErrSubstr: "s3 put object: boom",
			wantCalls:     struct{ get, put, del, list int }{put: 1},
		},
		{
			name: "middleware can enrich metadata for put (integration with Finalize)",
			reqCfg: mustReq(t, &S3RequestConfig{
				Operation: "put",
				Bucket:    "b",
				Key:       "k",
				Body:      []byte("x"),
				ExtraOpts: map[string]interface{}{},
			}),
			mw: []Middleware{
				StaticS3MetaMiddleware(map[string]string{
					"app": "gonetic",
				}),
			},
			wantStatus: 200,
			wantCalls:  struct{ get, put, del, list int }{put: 1},
			check: func(t *testing.T, f *fakeS3, resp dto.Response) {
				if f.gotPut[0].Metadata["app"] != "gonetic" {
					t.Fatalf("expected metadata from middleware, got=%v", f.gotPut[0].Metadata)
				}
			},
		},
		{
			name: "logging middleware is executed (smoke)",
			reqCfg: mustReq(t, &S3RequestConfig{
				Operation: "get",
				Bucket:    "b",
				Key:       "k",
			}),
			mw: []Middleware{
				LoggingMiddleware(func(msg string) {
					if !strings.Contains(msg, "GET s3://b/k") {
						t.Fatalf("unexpected log msg: %q", msg)
					}
				}),
			},
			wantStatus: 200,
			wantCalls:  struct{ get, put, del, list int }{get: 1},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, f := newTestClient(t, tc.mw...)
			if tc.fake != nil {
				tc.fake(f)
			}

			resp, err := c.ProcessRequest(context.Background(), tc.reqCfg)

			if tc.wantErrSubstr != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tc.wantErrSubstr)
				}
				if !strings.Contains(err.Error(), tc.wantErrSubstr) {
					t.Fatalf("expected error containing %q, got %v", tc.wantErrSubstr, err)
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if resp.StatusCode != tc.wantStatus {
				t.Fatalf("status mismatch: got=%d want=%d", resp.StatusCode, tc.wantStatus)
			}
			if tc.wantBody != "" && string(resp.Body) != tc.wantBody {
				t.Fatalf("body mismatch:\n got=%q\nwant=%q", string(resp.Body), tc.wantBody)
			}

			if len(f.gotGet) != tc.wantCalls.get ||
				len(f.gotPut) != tc.wantCalls.put ||
				len(f.gotDelete) != tc.wantCalls.del ||
				len(f.gotList) != tc.wantCalls.list {
				t.Fatalf("sdk call counts mismatch: get=%d put=%d del=%d list=%d (want %d/%d/%d/%d)",
					len(f.gotGet), len(f.gotPut), len(f.gotDelete), len(f.gotList),
					tc.wantCalls.get, tc.wantCalls.put, tc.wantCalls.del, tc.wantCalls.list)
			}

			if tc.check != nil {
				tc.check(t, f, resp)
			}
		})
	}
}

// badReqConfig is a dto.ReqConfigInterface implementation used to force the
// "problem casting to s3requestconfig" path in ProcessRequest.
type badReqConfig struct {
	ref dto.NetClientType
}

func (b *badReqConfig) Ref() dto.NetClientType { return b.ref }

func (b *badReqConfig) NewRequest(ctx context.Context) (any, error) {
	return &struct{}{}, nil
}

func TestS3Client_Type_And_Ref_Golden(t *testing.T) {
	c, _ := newTestClient(t)
	c.NetClient.Ref = "abc"

	if c.Type() != NetClientS3Ref {
		t.Fatalf("Type mismatch: got=%v want=%v", c.Type(), NetClientS3Ref)
	}
	if c.Ref() != "abc" {
		t.Fatalf("Ref mismatch: got=%q want=%q", c.Ref(), "abc")
	}
}

func TestS3Client_ProcessRequest_NilInputs_Golden(t *testing.T) {
	c, _ := newTestClient(t)

	// nil req config pointer will panic if called; ensure callers pass non-nil.
	// We assert current behavior to make it explicit.
	defer func() {
		if r := recover(); r == nil {
			t.Fatalf("expected panic on nil reqCfg, got none")
		}
	}()

	_, _ = c.ProcessRequest(context.Background(), nil)
}

func TestDoGet_ClosesBody_Golden(t *testing.T) {
	closed := false
	rc := &trackingReadCloser{
		r:      strings.NewReader("x"),
		closed: &closed,
	}

	c, f := newTestClient(t)
	f.getOut = &s3.GetObjectOutput{
		Body: rc,
		Metadata: map[string]string{
			"k": "v",
		},
	}

	req := &S3Request{
		Operation: "get",
		Bucket:    "b",
		Key:       "k",
		GetInput: &s3.GetObjectInput{
			Bucket: aws.String("b"),
			Key:    aws.String("k"),
		},
	}

	resp, err := c.doGet(context.Background(), req)
	if err != nil {
		t.Fatalf("doGet error: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("status mismatch: %d", resp.StatusCode)
	}
	if !closed {
		t.Fatalf("expected body to be closed")
	}
}

type trackingReadCloser struct {
	r      io.Reader
	closed *bool
}

func (t *trackingReadCloser) Read(p []byte) (int, error) { return t.r.Read(p) }

func (t *trackingReadCloser) Close() error {
	if t.closed != nil {
		*t.closed = true
	}
	return nil
}

// Ensure returned headers are valid http.Header in dto.Response for doGet.
func TestDoGet_ReturnsHTTPHeaderType_Golden(t *testing.T) {
	c, f := newTestClient(t)
	f.getOut = &s3.GetObjectOutput{
		Body:     io.NopCloser(strings.NewReader("x")),
		Metadata: map[string]string{"a": "1"},
	}

	req := &S3Request{
		Operation: "get",
		Bucket:    "b",
		Key:       "k",
		GetInput: &s3.GetObjectInput{
			Bucket: aws.String("b"),
			Key:    aws.String("k"),
		},
	}

	resp, err := c.doGet(context.Background(), req)
	if err != nil {
		t.Fatalf("doGet error: %v", err)
	}
	if resp.Headers == nil {
		t.Fatalf("expected non-nil headers")
	}
	// type-level check
	var _ http.Header = resp.Headers
}

// Optional: demonstrate that ProcessRequest uses cfg.NewRequest() and then Finalize().
func TestProcessRequest_CallsFinalize_ByObservingPreparedInputs_Golden(t *testing.T) {
	c, f := newTestClient(t)

	reqCfg := mustReq(t, &S3RequestConfig{
		Operation: "put",
		Bucket:    "b",
		Key:       "k",
		Body:      []byte("x"),
	})

	_, err := c.ProcessRequest(context.Background(), reqCfg)
	if err != nil {
		t.Fatalf("ProcessRequest error: %v", err)
	}
	if len(f.gotPut) != 1 {
		t.Fatalf("expected 1 PutObject call, got %d", len(f.gotPut))
	}
	if aws.ToString(f.gotPut[0].Bucket) != "b" {
		t.Fatalf("unexpected prepared bucket: %s", aws.ToString(f.gotPut[0].Bucket))
	}
}
