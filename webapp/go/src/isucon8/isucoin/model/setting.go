package model

import (
	"isucon8/isubank"
	"isucon8/isulogger"
	"log"
)

const (
	BankEndpoint = "bank_endpoint"
	BankAppid    = "bank_appid"
	LogEndpoint  = "log_endpoint"
	LogAppid     = "log_appid"
)

//go:generate scanner
type Setting struct {
	Name string
	Val  string
}

type InternalSetting struct {
	bankEndpoint string
	bankAppid    string
	logEndpoint  string
	logAppid     string
}

var (
	internalSetting InternalSetting
)

func SetSetting(d QueryExecutor, k, v string) error {
	_, err := d.Exec(`INSERT INTO setting (name, val) VALUES (?, ?) ON DUPLICATE KEY UPDATE val = VALUES(val)`, k, v)
	return err
}

func SyncSetting(d QueryExecutor) error {
	s, err := scanSetting(d.Query(`SELECT * FROM setting WHERE name = "bank_endpoint"`))
	if err != nil {
		return err
	}
	internalSetting.bankEndpoint = s.Val

	s, err = scanSetting(d.Query(`SELECT * FROM setting WHERE name = "bank_appid"`))
	if err != nil {
		return err
	}
	internalSetting.bankAppid = s.Val

	s, err = scanSetting(d.Query(`SELECT * FROM setting WHERE name = "log_endpoint"`))
	if err != nil {
		return err
	}
	internalSetting.logEndpoint = s.Val

	s, err = scanSetting(d.Query(`SELECT * FROM setting WHERE name = "log_appid"`))
	if err != nil {
		return err
	}
	internalSetting.logAppid = s.Val

	return nil
}

func Isubank(d QueryExecutor) (*isubank.Isubank, error) {
	return isubank.NewIsubank(internalSetting.bankEndpoint, internalSetting.logAppid)
}

func Logger(d QueryExecutor) (*isulogger.Isulogger, error) {
	return isulogger.NewIsulogger(internalSetting.logEndpoint, internalSetting.logAppid)
}

func sendLog(d QueryExecutor, tag string, v interface{}) {
	logger, err := Logger(d)
	if err != nil {
		log.Printf("[WARN] new logger failed. tag: %s, v: %v, err:%s", tag, v, err)
		return
	}
	err = logger.Send(tag, v)
	if err != nil {
		log.Printf("[WARN] logger send failed. tag: %s, v: %v, err:%s", tag, v, err)
	}
}
