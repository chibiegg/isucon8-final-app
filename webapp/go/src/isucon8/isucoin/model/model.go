package model

import (
	"database/sql"
	"log"
	"github.com/pkg/errors"
)

var (
	ErrBankUserNotFound   = errors.New("bank user not found")
	ErrBankUserConflict   = errors.New("bank user conflict")
	ErrUserNotFound       = errors.New("user not found")
	ErrOrderNotFound      = errors.New("order not found")
	ErrOrderAlreadyClosed = errors.New("order is already closed")
	ErrCreditInsufficient = errors.New("銀行の残高が足りません")
	ErrParameterInvalid   = errors.New("parameter invalid")
	ErrNoOrderForTrade    = errors.New("no order for trade")
)

type QueryExecutor interface {
	Exec(string, ...interface{}) (sql.Result, error)
	Query(string, ...interface{}) (*sql.Rows, error)
}

func InitBenchmark(d QueryExecutor) error {
	for _, q := range []string{
		"DELETE FROM orders WHERE created_at >= '2018-10-16 10:00:00'",
		"DELETE FROM trade WHERE created_at >= '2018-10-16 10:00:00'",
		"DELETE FROM user WHERE created_at >= '2018-10-16 10:00:00'",
	} {
		if _, err := d.Exec(q); err != nil {
			return errors.Wrapf(err, "query exec failed[%d]", q)
		}
	}
	return nil
}

func WarmDatabase(d QueryExecutor) error {
	log.Println("[DEBUG] Start warm database")
	for _, q := range []string{
		"SELECT * FROM user ORDER BY id",
		"SELECT * FROM trade ORDER BY id DESC",
		"SELECT * FROM orders ORDER BY id DESC",
	} {
		if _, err := d.Exec(q); err != nil {
			return errors.Wrapf(err, "query exec failed[%d]", q)
		}
	}

	log.Println("[DEBUG] Finished warm database")
	return nil
}
