package model

import (
	"database/sql"
	"fmt"
	"isucon8/isubank"
	"log"
	"sync"
	"time"

	"github.com/pkg/errors"
)

//go:generate scanner
type Trade struct {
	ID           int64     `json:"id"`
	Amount       int64     `json:"amount"`
	Price        int64     `json:"price"`
	CreatedAt    time.Time `json:"created_at"`
	CreatedAtSec time.Time `json:"created_at_sec"`
	CreatedAtMin time.Time `json:"created_at_min"`
	CreatedAtHou time.Time `json:"created_at_hou"`
}

//go:generate scanner
type CandlestickData struct {
	Time  time.Time `json:"time"`
	Open  int64     `json:"open"`
	Close int64     `json:"close"`
	High  int64     `json:"high"`
	Low   int64     `json:"low"`
}

type CandlestickStore struct {
	tcMap        map[time.Time]*CandlestickData
	delta        time.Duration
	cachedLatest time.Time
	m            *sync.Mutex
}

var (
	tcDeltaMap map[string]*CandlestickStore
)

func InitTcMap(d QueryExecutor) {
	log.Println("init tc map")
	BaseTime := time.Date(2018, 10, 16, 10, 0, 0, 0, time.Local)

	tcDeltaMap = make(map[string]*CandlestickStore)
	tcDeltaMap["created_at_sec"] = &CandlestickStore{make(map[time.Time]*CandlestickData), time.Second, BaseTime.Add(-400 * time.Second), new(sync.Mutex)}
	tcDeltaMap["created_at_min"] = &CandlestickStore{make(map[time.Time]*CandlestickData), time.Minute, BaseTime.Add(-400 * time.Minute), new(sync.Mutex)}
	tcDeltaMap["created_at_hou"] = &CandlestickStore{make(map[time.Time]*CandlestickData), time.Hour, BaseTime.Add(-400 * time.Hour), new(sync.Mutex)}

	for k, v := range tcDeltaMap {
		v.GetAndCacheIfPossibleInter(d, BaseTime.Add(v.delta*-300), k)
	}

}

func GetAndCacheIfPossible(d QueryExecutor, mt time.Time, colName string) ([]*CandlestickData, error) {
	return tcDeltaMap[colName].GetAndCacheIfPossibleInter(d, mt, colName)
}

func (cs *CandlestickStore) GetAndCacheIfPossibleInter(d QueryExecutor, mt time.Time, colName string) ([]*CandlestickData, error) {
	threshold := time.Now()
	if cs.delta == time.Second {
		threshold = time.Date(threshold.Year(), threshold.Month(), threshold.Day(), threshold.Hour(), threshold.Minute(), threshold.Second(), 0, time.Local)
	} else if cs.delta == time.Minute {
		threshold = time.Date(threshold.Year(), threshold.Month(), threshold.Day(), threshold.Hour(), threshold.Minute(), 0, 0, time.Local)
	} else if cs.delta == time.Hour {
		threshold = time.Date(threshold.Year(), threshold.Month(), threshold.Day(), threshold.Hour(), 0, 0, 0, time.Local)
	}

	possibleToCache := threshold

	// log.Println("calling GetCandlestickData")

	res, err := GetCandlestickData(d, cs.cachedLatest, colName)
	if err != nil {
		return nil, err
	}

	// log.Println("called GetCandlestickData")

	// res MUST be sorted by time
	// cs.m.Lock()
	// defer cs.m.Unlock()

	for _, val := range res {
		if cs.cachedLatest.Before(val.Time) {
			cs.tcMap[val.Time] = val
			if val.Time.Before(possibleToCache) {
				cs.cachedLatest = val.Time
			}
		}
	}
	// log.Println("building ans")

	ans := make([]*CandlestickData, 0)

	curTime := mt
	for curTime.Before(possibleToCache) {
		val, ok := cs.tcMap[curTime]
		if ok {
			ans = append(ans, val)
		}

		curTime = curTime.Add(cs.delta)
	}

	//	log.Println("building ans")

	return ans, nil
}

func GetTradeByID(d QueryExecutor, id int64) (*Trade, error) {
	return scanTrade(d.Query("SELECT * FROM trade WHERE id = ?", id))
}

func GetLatestTrade(d QueryExecutor) (*Trade, error) {
	return scanTrade(d.Query("SELECT * FROM trade ORDER BY id DESC LIMIT 1"))
}

func GetCandlestickData(d QueryExecutor, mt time.Time, timeColumnName string) ([]*CandlestickData, error) {
	query := fmt.Sprintf(`
		SELECT m.t, a.price, b.price, m.h, m.l
		FROM (
			SELECT
				%s AS t,
				MIN(id) AS min_id,
				MAX(id) AS max_id,
				MAX(price) AS h,
				MIN(price) AS l
			FROM trade
			WHERE created_at >= ?
			GROUP BY t
		) m
		JOIN trade a ON a.id = m.min_id
		JOIN trade b ON b.id = m.max_id
		ORDER BY m.t
	`, timeColumnName)

	return scanCandlestickDatas(d.Query(query, mt))
}

func HasTradeChanceByOrder(d QueryExecutor, orderID int64) (bool, error) {
	order, err := GetOrderByID(d, orderID)
	if err != nil {
		return false, err
	}

	lowest, err := GetLowestSellOrder(d)
	switch {
	case err == sql.ErrNoRows:
		return false, nil
	case err != nil:
		return false, errors.Wrap(err, "GetLowestSellOrder")
	}

	highest, err := GetHighestBuyOrder(d)
	switch {
	case err == sql.ErrNoRows:
		return false, nil
	case err != nil:
		return false, errors.Wrap(err, "GetHighestBuyOrder")
	}

	switch order.Type {
	case OrderTypeBuy:
		if lowest.Price <= order.Price {
			return true, nil
		}
	case OrderTypeSell:
		if order.Price <= highest.Price {
			return true, nil
		}
	default:
		return false, errors.Errorf("other type [%s]", order.Type)
	}
	return false, nil
}

func reserveOrder(d QueryExecutor, order *Order, price int64) (int64, error) {
	bank, err := Isubank(d)
	if err != nil {
		return 0, errors.Wrap(err, "isubank init failed")
	}
	p := order.Amount * price
	if order.Type == OrderTypeBuy {
		p *= -1
	}

	id, err := bank.Reserve(order.User.BankID, p)
	if err != nil {
		if err == isubank.ErrCreditInsufficient {
			if derr := cancelOrder(d, order, "reserve_failed"); derr != nil {
				return 0, derr
			}
			sendLog(d, order.Type+".error", map[string]interface{}{
				"error":   err.Error(),
				"user_id": order.UserID,
				"amount":  order.Amount,
				"price":   price,
			})
			return 0, err
		}
		return 0, errors.Wrap(err, "isubank.Reserve")
	}

	return id, nil
}

func commitReservedOrder(tx *sql.Tx, order *Order, targets []*Order, reserves []int64) error {
	res, err := tx.Exec(`
INSERT INTO trade (amount, price, created_at, created_at_sec, created_at_min, created_at_hou)
VALUES (?, ?, NOW(6),
STR_TO_DATE(DATE_FORMAT(NOW(), '%Y-%m-%d %H:%i:%s'), '%Y-%m-%d %H:%i:%s'),
STR_TO_DATE(DATE_FORMAT(NOW(), '%Y-%m-%d %H:%i:00'), '%Y-%m-%d %H:%i:%s'),
STR_TO_DATE(DATE_FORMAT(NOW(), '%Y-%m-%d %H:00:00'), '%Y-%m-%d %H:%i:%s')
)`, order.Amount, order.Price)
	if err != nil {
		return errors.Wrap(err, "insert trade")
	}
	tradeID, err := res.LastInsertId()
	if err != nil {
		return errors.Wrap(err, "lastInsertID for trade")
	}
	sendLog(tx, "trade", map[string]interface{}{
		"trade_id": tradeID,
		"price":    order.Price,
		"amount":   order.Amount,
	})
	for _, o := range append(targets, order) {
		if _, err = tx.Exec(`UPDATE orders SET trade_id = ?, closed_at = NOW(6) WHERE id = ?`, tradeID, o.ID); err != nil {
			return errors.Wrap(err, "update order for trade")
		}
		sendLog(tx, o.Type+".trade", map[string]interface{}{
			"order_id": o.ID,
			"price":    order.Price,
			"amount":   o.Amount,
			"user_id":  o.UserID,
			"trade_id": tradeID,
		})
	}
	bank, err := Isubank(tx)
	if err != nil {
		return errors.Wrap(err, "isubank init failed")
	}
	if err = bank.Commit(reserves); err != nil {
		return errors.Wrap(err, "commit")
	}
	return nil
}

func tryTrade(tx *sql.Tx, orderID int64) error {
	order, err := getOpenOrderByID(tx, orderID)
	if err != nil {
		return err
	}

	restAmount := order.Amount
	unitPrice := order.Price
	reserves := make([]int64, 1, order.Amount+1)
	targets := make([]*Order, 0, order.Amount)

	reserves[0], err = reserveOrder(tx, order, unitPrice)
	if err != nil {
		return err
	}
	defer func() {
		if len(reserves) > 0 {
			bank, err := Isubank(tx)
			if err != nil {
				log.Printf("[WARN] isubank init failed. err:%s", err)
				return
			}
			if err = bank.Cancel(reserves); err != nil {
				log.Printf("[WARN] isubank cancel failed. err:%s", err)
			}
		}
	}()

	var targetOrders []*Order
	switch order.Type {
	case OrderTypeBuy:
		targetOrders, err = scanOrders(tx.Query(`SELECT * FROM orders WHERE type = ? AND closed_at IS NULL AND price <= ? ORDER BY price ASC, created_at ASC, id ASC`, OrderTypeSell, order.Price))
	case OrderTypeSell:
		targetOrders, err = scanOrders(tx.Query(`SELECT * FROM orders WHERE type = ? AND closed_at IS NULL AND price >= ? ORDER BY price DESC, created_at ASC, id ASC`, OrderTypeBuy, order.Price))
	}
	if err != nil {
		return errors.Wrap(err, "find target orders")
	}
	if len(targetOrders) == 0 {
		return ErrNoOrderForTrade
	}

	for _, to := range targetOrders {
		to, err = getOpenOrderByID(tx, to.ID)
		if err != nil {
			if err == ErrOrderAlreadyClosed {
				continue
			}
			return errors.Wrap(err, "getOpenOrderByID  buy_order")
		}
		if to.Amount > restAmount {
			continue
		}
		rid, err := reserveOrder(tx, to, unitPrice)
		if err != nil {
			if err == isubank.ErrCreditInsufficient {
				continue
			}
			return err
		}
		reserves = append(reserves, rid)
		targets = append(targets, to)
		restAmount -= to.Amount
		if restAmount == 0 {
			break
		}
	}
	if restAmount > 0 {
		return ErrNoOrderForTrade
	}
	if err = commitReservedOrder(tx, order, targets, reserves); err != nil {
		return err
	}
	reserves = reserves[:0]
	return nil
}

var (
	runRunTrade bool
)

func MarkRunTrade() {
	runRunTrade = true
}

func DemarkRunTrade() {
	runRunTrade = false
}

func StartRunTradeGoRoutine(db *sql.DB) {
	go func() {
		t := time.NewTicker(time.Millisecond * 600)
		for{
			<- t.C
			if runRunTrade {
				runRunTrade = false

				cnt, err := RunTrade(db)
				if err != nil {
					log.Println("error happened")
				}
				log.Println("RunTrade = ", cnt)
			}
		}
	}()

}

func RunTrade(db *sql.DB) (int, error) {
	lowestSellOrder, err := GetLowestSellOrder(db)
	switch {
	case err == sql.ErrNoRows:
		// 売り注文が無いため成立しない
		return 0, nil
	case err != nil:
		return 0, errors.Wrap(err, "GetLowestSellOrder")
	}

	highestBuyOrder, err := GetHighestBuyOrder(db)
	switch {
	case err == sql.ErrNoRows:
		// 買い注文が無いため成立しない
		return 0, nil
	case err != nil:
		return 0, errors.Wrap(err, "GetHighestBuyOrder")
	}

	if lowestSellOrder.Price > highestBuyOrder.Price {
		// 最安の売値が最高の買値よりも高いため成立しない
		return 0, nil
	}

	candidates := make([]int64, 0, 2)
	if lowestSellOrder.Amount > highestBuyOrder.Amount {
		candidates = append(candidates, lowestSellOrder.ID, highestBuyOrder.ID)
	} else {
		candidates = append(candidates, highestBuyOrder.ID, lowestSellOrder.ID)
	}

	for _, orderID := range candidates {
		err := func() error {
			tx, err := db.Begin()
			if err != nil {
				return errors.Wrap(err, "begin transaction failed")
			}
			err = tryTrade(tx, orderID)
			switch err {
			case nil, ErrNoOrderForTrade, ErrOrderAlreadyClosed, isubank.ErrCreditInsufficient:
				tx.Commit()
			default:
				tx.Rollback()
			}
			return err
		}()
		switch err {
		case nil:
			// トレード成立したため次の取引を行う
			cnt, err := RunTrade(db)
			return cnt + 1, err
		case ErrNoOrderForTrade, ErrOrderAlreadyClosed:
			// 注文個数の多い方で成立しなかったので少ない方で試す
			continue
		default:
			return 0, err
		}
	}
	// 個数のが不足していて不成立
	return 0, nil
}
