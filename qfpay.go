// Package qfpayslim provides a client to interact with QFPay API.
// Docs: https://sdk.qfapi.com/
package qfpayslim

import (
	"bytes"
	"context"
	"crypto/md5"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	PayTypeAlipayQRCode    = "800101" // Alipay Merchant Presented QR Code Payment in store (MPM) (Overseas Merchants)
	PayTypeWechatPayQRCode = "800201" // WeChat Merchant Presented QR Code Payment (MPM) (Overseas & HK Merchants)
	PayTypePayMeQRCode     = "805801" // PayMe Merchant Presented QR Code Payment in store (MPM) (HK Merchants)
	PayTypeFPSQRCode       = "802001" // FPS Merchant Presented QR Code Payment (MPM) (HK Merchants)
	PayTypeAlipayAPP       = "801510" // Alipay In-App Payment (HK Merchants)
	PayTypeAlipayWAP       = "801512" // Alipay Online WAP Payment (HK Merchants)
)

// Client struct is used to interact with QFPay API.
type Client struct {
	Prefix  string // https://openapi-hk.qfapi.com or https://test-openapi-hk.qfapi.com
	AppCode string // 32-character string
	Key     string // 32-character string
	Debug   bool   // show request and response body
}

type Request struct {
	*http.Request
	client *Client
}

// QFError represents an API error response from QFPay.
type QFError struct {
	Code     string `json:"respcd"`
	Err      string `json:"resperr"`
	Messsage string `json:"respmsg"`
}

func (e QFError) Error() string {
	err := e.Err
	if e.Messsage != "" {
		err = err + " (" + e.Messsage + ")"
	}
	return "Error: Code=" + e.Code + ", Message=" + err
}

// NewRequest creates a new HTTP request with context, method, URL, and body.
// If the request body is already an `io.Reader`, it is used as-is. Otherwise,
// the request body is serialized into JSON format.
func (c *Client) NewRequest(ctx context.Context, method, url string, reqBody interface{}) (*Request, error) {
	r, err := reqBodyToReader(reqBody)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, method, c.Prefix+url, r)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	return &Request{req, c}, nil
}

// MakePayment creates a payment request to the QFPay API.
// It accepts payment type, transaction number, item name, and amount in cents.
func (c *Client) MakePayment(ctx context.Context, payType, outTradeNo, goodsName string, cents int, extra map[string]string) (*Request, error) {
	payload := url.Values{}
	payload.Set("txamt", strconv.Itoa(cents))
	payload.Set("txcurrcd", "HKD")
	payload.Set("pay_type", payType)
	payload.Set("out_trade_no", outTradeNo)
	payload.Set("goods_name", goodsName)
	payload.Set("txdtm", time.Now().UTC().Format("2006-01-02 15:04:05"))
	for k, v := range extra {
		payload.Set(k, v)
	}
	req, err := c.NewRequest(ctx, "POST", "/trade/v1/payment", strings.NewReader(payload.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("X-QF-APPCODE", c.AppCode)
	req.Header.Set("X-QF-SIGN", c.GenerateSign(payload))
	req.Header.Set("X-QF-SIGNTYPE", "MD5")
	return req, nil
}

// QueryResponse holds the information returned from QFPay API for a payment request.
// Fields included match the JSON response properties returned from the API.
type QueryResponse struct {
	Cancel      string `json:"cancel"`       // Cancellation or refund indicator
	Cardcd      string `json:"cardcd"`       // Card number
	Cardtp      string `json:"cardtp"`       // Unknown
	Chnlsn      string `json:"chnlsn"`       // Wallet/Channel transaction number
	Chnlsn2     string `json:"chnlsn2"`      // Additional transaction number added to the order
	Clisn       string `json:"clisn"`        // Unknown
	Errmsg      string `json:"errmsg"`       // Payment status message
	GoodsDetail string `json:"goods_detail"` // Product details
	GoodsInfo   string `json:"goods_info"`   // Product description
	GoodsName   string `json:"goods_name"`   // Product name
	OrderType   string `json:"order_type"`   // Order type (payment / refund)
	OutTradeNo  string `json:"out_trade_no"` // API order number
	PayType     string `json:"pay_type"`     // Payment type
	Paydtm      string `json:"paydtm"`       // Payment time of the transaction
	Respcd      string `json:"respcd"`       // Payment status
	Sysdtm      string `json:"sysdtm"`       // System transaction time
	Syssn       string `json:"syssn"`        // QFPay transaction number
	Txamt       string `json:"txamt"`        // Transaction amount
	Txcurrcd    string `json:"txcurrcd"`     // Transaction currency
	Txdtm       string `json:"txdtm"`        // Request transaction time
	Udid        string `json:"udid"`         // Unique transaction device ID
	Userid      string `json:"userid"`       // User ID
}

func (res QueryResponse) Paid() bool {
	return res.Respcd == "0000"
}

// Query sends a request to inquire about past payment transactions.
// Multiple transaction numbers can be queried in a single request by passing them as separate
// arguments.
func (c *Client) Query(ctx context.Context, outTradeNo ...string) ([]QueryResponse, error) {
	if len(outTradeNo) < 1 {
		return nil, nil
	}
	payload := url.Values{}
	payload.Set("out_trade_no", strings.Join(outTradeNo, ","))
	req, err := c.NewRequest(ctx, "POST", "/trade/v1/query", strings.NewReader(payload.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("X-QF-APPCODE", c.AppCode)
	req.Header.Set("X-QF-SIGN", c.GenerateSign(payload))
	req.Header.Set("X-QF-SIGNTYPE", "MD5")
	var responses []QueryResponse
	err = req.Do(&responses, "data.*")
	return responses, err
}

// GenerateSign generates a signature for authenticating API requests.
func (c *Client) GenerateSign(payload url.Values) string {
	parts := make([]string, len(payload))
	i := 0
	for k := range payload {
		parts[i] = k + "=" + payload.Get(k)
		i += 1
	}
	sort.Strings(parts)
	joined := strings.Join(parts, "&") + c.Key
	return fmt.Sprintf("%X", md5.Sum([]byte(joined)))
}

// Do sends the HTTP request associated with the Request object.
// If successful, it unmarshals the returned data into the specified destination(s).
// The destination can be a struct to hold the unmarshalled JSON response, or a set
// of pointers that point to variables to store pieces of data identified by corresponding JSON
// keys.
//
// If the 'dest' slice contains more than one value, they are expected to come in pairs with a
// pointer
// to a variable followed by the associated JSON key string.
//
// If the 'dest' slice contains only one element, it should be a pointer to a struct or a []byte
// where the entire response can be stored.
//
// It handles QFPay-specific error responses and returns a nil error on successful requests.
//
// If Debug is enabled on the Client, the function will log HTTP request and response details.
func (req *Request) Do(dest ...interface{}) error {
	if req.client.Debug {
		dump, err := httputil.DumpRequestOut(req.Request, true)
		if err != nil {
			return err
		}
		log.Println(string(dump))
	}
	res, err := http.DefaultClient.Do(req.Request)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if req.client.Debug {
		dumpBody := strings.Contains(res.Header.Get("Content-Type"), "json")
		dump, err := httputil.DumpResponse(res, dumpBody)
		if err != nil {
			return err
		}
		log.Println(string(dump))
	}
	b, err := ioutil.ReadAll(res.Body)
	if err != nil {
		return err
	}
	var respError QFError
	json.Unmarshal(b, &respError)
	if respError.Code != "0000" {
		return respError
	}
	if len(dest) == 0 {
		return nil
	}
	if len(dest) > 1 {
		for n := 0; n < len(dest)/2; n++ {
			arrange(b, dest[2*n], dest[2*n+1].(string))
		}
		return nil
	}
	if x, ok := dest[0].(*[]byte); ok {
		*x = b
		return nil
	}
	return json.Unmarshal(b, dest[0])
}

func reqBodyToReader(reqBody interface{}) (io.Reader, error) {
	if reqBody == nil {
		return nil, nil
	}
	if r, ok := reqBody.(io.Reader); ok {
		return r, nil
	}
	b, err := json.Marshal(reqBody)
	if err != nil {
		return nil, err
	}
	return bytes.NewReader(b), nil
}

func arrange(data []byte, target interface{}, key string) {
	keys := strings.Split(key, ".")
	baseType := reflect.TypeOf(target).Elem()
	if baseType.Kind() == reflect.Slice {
		baseType = baseType.Elem()
	}
	typ := baseType
	for i := len(keys) - 1; i > -1; i-- {
		key := keys[i]
		if key == "*" {
			typ = reflect.SliceOf(typ)
		} else if key != "" {
			typ = reflect.MapOf(reflect.TypeOf(key), typ)
		}
	}
	d := reflect.New(typ)
	json.Unmarshal(data, d.Interface())
	items := collect(d.Elem(), keys)
	v := reflect.Indirect(reflect.ValueOf(target))
	if !v.IsValid() {
		return
	}
	for n := range items {
		item := items[n]
		if !item.IsValid() {
			item = reflect.New(baseType).Elem()
		}
		if v.Kind() == reflect.Slice {
			v.Set(reflect.Append(v, item))
		} else {
			v.Set(item)
		}
	}
}

func collect(x reflect.Value, keys []string) (out []reflect.Value) {
	for i, key := range keys {
		if key == "*" {
			k := keys[i+1:]
			for i := 0; i < x.Len(); i++ {
				out = append(out, collect(x.Index(i), k)...)
			}
			return
		} else if key != "" {
			x = x.MapIndex(reflect.ValueOf(key))
		}
	}
	out = append(out, x)
	return
}
