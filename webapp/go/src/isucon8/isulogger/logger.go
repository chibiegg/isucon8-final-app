// Package isulogger is client for ISULOG
package isulogger

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"path"
	"log"
	"time"
)

// Log はIsuloggerに送るためのログフォーマット
type Log struct {
	// Tagは各ログを識別するための情報です
	Tag string `json:"tag"`
	// Timeはログの発生時間
	Time time.Time `json:"time"`
	// Data はログの詳細情報でTagごとに決められています
	Data interface{} `json:"data"`
}

type Isulogger struct {
	endpoint *url.URL
	appID    string
	queue    chan *Log
}

var isulogger *Isulogger


func InitializeIsulogger(endpoint, appID string) error {
	u, err := url.Parse(endpoint)
	if err != nil {
		return err
	}

	queue := make(chan *Log, 10)
	isulogger = &Isulogger{
		endpoint: u,
		appID:    appID,
		queue:    queue,
	}

	go isulogger.Loop()
	return nil
}


// NewIsulogger はIsuloggerを初期化します
//
// endpoint: ISULOGを利用するためのエンドポイントURI
// appID:    ISULOGを利用するためのアプリケーションID
func NewIsulogger(endpoint, appID string) (*Isulogger, error) {
	if isulogger == nil {
		err := InitializeIsulogger(endpoint, appID)
		if err != nil {
			return nil, err
		}
	}
	isulogger.update(endpoint,appID)
	return isulogger, nil
}

func (b *Isulogger) Loop() {
	t := time.NewTicker(200 * time.Millisecond) // 200ms秒おきに送信
	messages := make([]*Log, 0)

	for {
		select {
		case l := <- b.queue:
		    messages = append(messages, l)
		case <-t.C:
				if len(messages) > 0 {
					b.request("/send_bulk", messages)
					log.Printf("[DEBUG] send_bulk %d", len(messages))
					messages = make([]*Log, 0)
				}
		}
	}
}

// Send はログを送信します
func (b *Isulogger) Send(tag string, data interface{}) error {
	message := &Log{tag, time.Now(), data}
	b.queue <- message
	return nil
}

func (b *Isulogger) update(endpoint, appID string) {
		u, err := url.Parse(endpoint)
		if err != nil {
			return
		}

		isulogger.endpoint = u
		isulogger.appID = appID
}


func (b *Isulogger) request(p string, v interface{}) error {
	u := new(url.URL)
	*u = *b.endpoint
	u.Path = path.Join(u.Path, p)

	log.Println("[DEBUG] isulogger %s", u)
	body := &bytes.Buffer{}
	if err := json.NewEncoder(body).Encode(v); err != nil {
		return fmt.Errorf("logger json encode failed. err: %s", err)
	}

	req, err := http.NewRequest(http.MethodPost, u.String(), body)
	if err != nil {
		return fmt.Errorf("logger new request failed. err: %s", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+b.appID)

	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("logger request failed. err: %s", err)
	}
	defer res.Body.Close()
	bo, err := ioutil.ReadAll(res.Body)
	if err != nil {
		return fmt.Errorf("logger body read failed. err: %s", err)
	}
	if res.StatusCode == http.StatusOK {
		return nil
	}
	return fmt.Errorf("logger status is not ok. code: %d, body: %s", res.StatusCode, string(bo))
}
