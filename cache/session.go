package cache

import (
	"bytes"
	"context"
	"io"
	"io/ioutil"
	"time"

	"github.com/ticketmaster/aws-sdk-go-cache/timing"

	"github.com/aws/aws-sdk-go/aws/awsutil"

	"github.com/aws/aws-sdk-go/aws/request"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/karlseguin/ccache"
)

type contextKeyType int

var cacheContextKey = new(contextKeyType)

const ttl = 10 * time.Second

var cache *ccache.Cache

type cacheObject struct {
	body io.ReadCloser
	req  *request.Request
}

func cacheKey(a, b, c string) string {
	return a + "." + b + "." + c
}

var cacheHandler = request.NamedHandler{
	Name: "cache.cacheHandler",
	Fn: func(r *request.Request) {
		i := cache.Get(cacheKey(r.ClientInfo.ServiceName, r.Operation.Name, awsutil.Prettify(r.Params)))

		if i != nil && !i.Expired() {
			v := i.Value().(*cacheObject)

			// Copy cached data to this request
			r.HTTPResponse = v.req.HTTPResponse
			r.HTTPResponse.Body = v.body

			// Set value in context to mark that this is a cached result
			r.HTTPRequest = r.HTTPRequest.WithContext(context.WithValue(r.HTTPRequest.Context(), cacheContextKey, true))

			// Adjust start time of HTTP request since the httptrace ConnectStart will not be executed
			td := timing.GetData(r.HTTPRequest.Context())
			td.SetConnectionStart(time.Now())
		} else {
			// Cache a copy of the HTTP response body
			bodyBytes, _ := ioutil.ReadAll(r.HTTPResponse.Body)
			cacheBody := ioutil.NopCloser(bytes.NewBuffer(bodyBytes))
			r.HTTPResponse.Body = ioutil.NopCloser(bytes.NewBuffer(bodyBytes))

			o := &cacheObject{
				body: cacheBody,
				req:  r,
			}

			cache.Set(cacheKey(r.ClientInfo.ServiceName, r.Operation.Name, awsutil.Prettify(r.Params)), o, ttl)
		}
	},
}

// AddCaching adds caching to the Session
func AddCaching(s *session.Session) {
	cache = ccache.New(ccache.Configure())
	s.Handlers.Send.AfterEachFn = func(item request.HandlerListRunItem) bool {
		i := cache.Get(cacheKey(item.Request.ClientInfo.ServiceName, item.Request.Operation.Name, awsutil.Prettify(item.Request.Params)))
		if i != nil && !i.Expired() {
			return false
		}
		return true
	}

	s.Handlers.ValidateResponse.PushFrontNamed(cacheHandler)
}

// IsCacheHit returns true if the context was used for a cached API request
func IsCacheHit(ctx context.Context) bool {
	cached := ctx.Value(cacheContextKey)
	return cached != nil
}
