package integrations

import (
	"bytes"
	"context"
	"crypto/aes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/iag-finance/backend/internal/config"
)

// uraS2SEFRIS posts T109-style envelopes to URA EFRIS web services.
// See docs/URA_EFRIS.md for configuration and certificate requirements.
type uraS2SEFRIS struct {
	s2sURL     string
	s2sPath    string
	tin        string
	deviceNo   string
	branchID   string
	aesKey     []byte
	httpClient *http.Client
}

func (u *uraS2SEFRIS) Mode() string { return "ura_s2s" }

func (u *uraS2SEFRIS) Ping(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(u.s2sURL, "/")+u.s2sPath, nil)
	if err != nil {
		return err
	}
	resp, err := u.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 500 {
		return fmt.Errorf("ura s2s health %s", resp.Status)
	}
	return nil
}

type uraGlobalInfo struct {
	Version         string `json:"version"`
	DataExchangeID  string `json:"dataExchangeId"`
	InterfaceCode   string `json:"interfaceCode"`
	RequestCode     string `json:"requestCode"`
	RequestTime     string `json:"requestTime"`
	ResponseCode    string `json:"responseCode"`
	UserName        string `json:"userName"`
	DeviceNo        string `json:"deviceNo"`
	Tin             string `json:"tin"`
	TaxpayerID      string `json:"taxpayerID"`
	BranchID        string `json:"branchId"`
}

type uraInvoicePayload struct {
	DocumentRef string `json:"documentRef"`
	Amount      string `json:"amount"`
	Currency    string `json:"currency"`
	CustomerRef string `json:"customerRef"`
}

func (u *uraS2SEFRIS) Submit(ctx context.Context, in EFRISSubmitRequest) (EFRISSubmitResult, error) {
	now := time.Now().UTC().Format("2006-01-02 15:04:05")
	payload := uraInvoicePayload{
		DocumentRef: in.DocumentRef,
		Amount:      in.Amount,
		Currency:    in.Currency,
		CustomerRef: in.CustomerRef,
	}
	raw, _ := json.Marshal(payload)
	dataField := base64.StdEncoding.EncodeToString(raw)
	if len(u.aesKey) > 0 {
		enc, err := aesEncryptECB(raw, u.aesKey)
		if err != nil {
			return EFRISSubmitResult{Status: "failed", ErrorMessage: err.Error()}, nil
		}
		dataField = base64.StdEncoding.EncodeToString(enc)
	}

	envelope := map[string]any{
		"data": dataField,
		"globalInfo": uraGlobalInfo{
			Version:        "1.0",
			DataExchangeID: fmt.Sprintf("IAG-%s-%d", in.DocumentRef, time.Now().Unix()),
			InterfaceCode:  "T109",
			RequestCode:    "TP",
			RequestTime:    now,
			ResponseCode:   "",
			UserName:       u.tin,
			DeviceNo:       u.deviceNo,
			Tin:            u.tin,
			TaxpayerID:     u.tin,
			BranchID:       u.branchID,
		},
	}
	body, _ := json.Marshal(envelope)
	url := strings.TrimRight(u.s2sURL, "/") + u.s2sPath
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return EFRISSubmitResult{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := u.httpClient.Do(req)
	if err != nil {
		return EFRISSubmitResult{Status: "failed", ErrorMessage: err.Error()}, nil
	}
	defer resp.Body.Close()
	rawResp, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return EFRISSubmitResult{Status: "failed", ErrorMessage: strings.TrimSpace(string(rawResp))}, nil
	}

	var parsed struct {
		ReturnStateInfo struct {
			ReturnCode string `json:"returnCode"`
			ReturnMsg  string `json:"returnMessage"`
		} `json:"returnStateInfo"`
		Data string `json:"data"`
	}
	_ = json.Unmarshal(rawResp, &parsed)
	if parsed.ReturnStateInfo.ReturnCode != "" && parsed.ReturnStateInfo.ReturnCode != "00" {
		msg := parsed.ReturnStateInfo.ReturnMsg
		if msg == "" {
			msg = string(rawResp)
		}
		return EFRISSubmitResult{Status: "failed", ErrorMessage: msg}, nil
	}

	receipt := in.DocumentRef
	if parsed.Data != "" {
		decoded, err := base64.StdEncoding.DecodeString(parsed.Data)
		if err == nil {
			var ack struct {
				InvoiceNo string `json:"invoiceNo"`
				Receipt   string `json:"receipt"`
			}
			if json.Unmarshal(decoded, &ack) == nil {
				if ack.Receipt != "" {
					receipt = ack.Receipt
				} else if ack.InvoiceNo != "" {
					receipt = ack.InvoiceNo
				}
			}
		}
	}
	return EFRISSubmitResult{Status: "acknowledged", URAReceipt: receipt}, nil
}

// aesEncryptECB encrypts with AES-ECB + PKCS#7. ECB is intentional here: the
// URA EFRIS T109 server-to-server specification mandates AES/ECB/PKCS5Padding
// for the request payload. It is required for interoperability with URA and
// must not be changed to CBC/GCM without a corresponding URA spec change.
func aesEncryptECB(plain, key []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	padded := pkcs7Pad(plain, block.BlockSize())
	out := make([]byte, len(padded))
	for i := 0; i < len(padded); i += block.BlockSize() {
		block.Encrypt(out[i:i+block.BlockSize()], padded[i:i+block.BlockSize()])
	}
	return out, nil
}

func pkcs7Pad(data []byte, blockSize int) []byte {
	pad := blockSize - len(data)%blockSize
	p := bytes.Repeat([]byte{byte(pad)}, pad)
	return append(data, p...)
}

func newURAS2SEFRIS(cfg config.Config) EFRISAdapter {
	key := []byte(strings.TrimSpace(cfg.EFRISAESKey))
	if len(key) != 16 && len(key) != 24 && len(key) != 32 {
		key = nil
	}
	path := cfg.EFRISS2SPath
	if path == "" {
		path = "/efrisws/ws/ta/request"
	}
	url := cfg.EFRISS2SURL
	if url == "" {
		url = "https://efrisws.ura.go.ug"
	}
	return &uraS2SEFRIS{
		s2sURL:     url,
		s2sPath:    path,
		tin:        cfg.EFRISTIN,
		deviceNo:   cfg.EFRISDeviceNo,
		branchID:   cfg.EFRISBranchID,
		aesKey:     key,
		httpClient: &http.Client{Timeout: 45 * time.Second},
	}
}
