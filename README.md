# qfpayslim

Usage:

```go
qfpay := qfpayslim.Client{
    Prefix:  "https://openapi-hk.qfapi.com",
    AppCode: "XXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX",
    Key:     "XXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX",
    // Debug:   true,
}
ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
defer cancel()
req, err := qfpay.MakePayment(ctx, qfpayslim.PayTypePayMeQRCode, "TradeNo", "ProductName", 100)
if err != nil {
    return err
}
var qrcode, qfpaytxid string
if err := req.Do(&qrcode, "qrcode", &qfpaytxid, "syssn"); err != nil {
    return err
}


responses, err := qfpay.Query(ctx, "TradeNo", "TradeNo2", ...)
if err != nil {
    return err
}
```
