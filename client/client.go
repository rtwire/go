package client

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

const (
	MainNetURL  = "https://api.rtwire.com/v1/mainnet"
	TestNet3URL = "https://api/rtwire.com/v1/testnet3"
)

var (
	ErrTxIDUsed          = errors.New("TxID used")
	ErrInsufficientFunds = errors.New("insufficient funds")
	ErrHookExists        = errors.New("hook exists")
)

type TransactionType string

const (
	Transfer TransactionType = "transfer"
	Debit    TransactionType = "debit"
	Credit   TransactionType = "credit"
)

type option func(url *url.URL) error

func setQueryValue(u *url.URL, key, value string) error {
	v, err := url.ParseQuery(u.RawQuery)
	if err != nil {
		return err
	}
	v.Set(key, value)
	u.RawQuery = v.Encode()
	return nil
}

func Limit(limit int) option {
	return func(u *url.URL) error {
		return setQueryValue(u, "limit", strconv.Itoa(limit))
	}
}

func Next(next string) option {
	return func(u *url.URL) error {
		return setQueryValue(u, "next", next)
	}
}

func Pending() option {
	return func(u *url.URL) error {
		return setQueryValue(u, "status", "pending")
	}
}

type Client interface {
	CreateAccount() (Account, error)
	Account(accountID int64) (Account, error)
	Accounts(options ...option) ([]Account, error)

	CreateAddress(accountID int64) (string, error)

	CreateTransactionIDs(int) ([]int64, error)
	Transaction(txID int64) (Transaction, error)
	AccountTransactions(accountID int64, options ...option) (
		string, []Transaction, error)

	Transfer(txID, fromAccountID, toAccountID, value int64) error

	Debit(txID, fromAccountID int64, toAddress string, value int64) error

	CreateHook(url string) error
	Hooks() ([]Hook, error)
	DeleteHook(url string) error
}

type client struct {
	client *http.Client
	url    string
	user   string
	pass   string
}

type Account struct {
	ID      int64 `json:"id"`
	Balance int64 `json:"balance"`
}

type Transaction struct {
	ID   int64
	Type TransactionType

	FromAccountID int64 `json:"fromAccountID"`
	ToAccountID   int64 `json:"toAccountID"`

	FromAccountBalance int64 `json:"fromAccountBalance"`
	ToAccountBalance   int64 `json:"toAccountBalance"`

	FromAccountTxID int64 `json:"fromAccountTxID"`
	ToAccountTxID   int64 `json:"toAccountTxID"`

	Value   int64
	Created time.Time

	TxHashes   []string `json:"txHashes"`
	TxOutIndex int64    `json:"txOutIndex"`
}

type Hook struct {
	URL string `json:"url"`
}

type address struct {
	Address string
}

type object struct {
	Type    string
	Next    string
	Payload json.RawMessage
}

func (c *client) do(req *http.Request) (string, json.RawMessage, error) {
	resp, err := c.client.Do(req)
	if err != nil {
		return "", nil, err
	}
	defer resp.Body.Close()

	// We don't care about the status code. Only if we can decode JSON.
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return "", nil, err
	}

	// Check if no response expected.
	if len(body) == 0 && resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return "", nil, nil
	}

	obj := &object{}
	if err := json.Unmarshal(body, obj); err != nil {
		return "", nil, fmt.Errorf("%v: %s", req.URL, body)
	}

	if obj.Type == "error" {
		return "", nil, doError(obj)
	}
	return obj.Next, obj.Payload, nil
}

func doError(obj *object) error {
	payload := make([]struct {
		Message string
	}, 0, 1)
	if err := json.Unmarshal(obj.Payload, &payload); err != nil {
		return err
	}
	return errors.New(payload[0].Message)
}

func accountsFromPayload(payload json.RawMessage) ([]Account, error) {
	accs := []Account{}
	if err := json.Unmarshal(payload, &accs); err != nil {
		return nil, err
	}
	return accs, nil
}

func accountFromPayload(payload json.RawMessage) (Account, error) {
	accs, err := accountsFromPayload(payload)
	if err != nil {
		return Account{}, err
	}
	if len(accs) != 1 {
		return Account{}, errors.New("expected one account")
	}
	return accs[0], nil
}

func (c *client) CreateAccount() (Account, error) {
	urlStr := fmt.Sprintf("%s/accounts/", c.url)
	req, err := http.NewRequest("POST", urlStr, nil)
	if err != nil {
		return Account{}, err
	}
	req.SetBasicAuth(c.user, c.pass)
	req.Header.Set("Accept", "application/json")
	_, payload, err := c.do(req)
	if err != nil {
		return Account{}, err
	}
	return accountFromPayload(payload)
}

func (c *client) Account(id int64) (Account, error) {
	urlStr := fmt.Sprintf("%s/accounts/%d", c.url, id)
	req, err := http.NewRequest("GET", urlStr, nil)
	if err != nil {
		return Account{}, err
	}
	req.SetBasicAuth(c.user, c.pass)
	req.Header.Set("Accept", "application/json")
	_, payload, err := c.do(req)
	if err != nil {
		return Account{}, err
	}

	return accountFromPayload(payload)
}

func (c *client) Accounts(options ...option) ([]Account, error) {

	urlStr := fmt.Sprintf("%s/accounts/", c.url)
	url, err := url.Parse(urlStr)
	if err != nil {
		return nil, err
	}

	for _, op := range options {
		if err := op(url); err != nil {
			return nil, err
		}
	}

	req, err := http.NewRequest("GET", url.String(), nil)
	if err != nil {
		return nil, err
	}
	req.SetBasicAuth(c.user, c.pass)
	req.Header.Set("Accept", "application/json")
	_, payload, err := c.do(req)
	if err != nil {
		return nil, err
	}

	return accountsFromPayload(payload)
}

func (c *client) CreateAddress(accountID int64) (string, error) {
	urlStr := fmt.Sprintf("%s/accounts/%d/addresses/", c.url, accountID)
	req, err := http.NewRequest("POST", urlStr, nil)
	if err != nil {
		return "", err
	}
	req.SetBasicAuth(c.user, c.pass)
	req.Header.Set("Accept", "application/json")
	_, payload, err := c.do(req)
	if err != nil {
		return "", err
	}
	addrs := []address{}
	if err := json.Unmarshal(payload, &addrs); err != nil {
		return "", err
	}

	if len(addrs) != 1 {
		return "", errors.New("expected one address")
	}
	return addrs[0].Address, nil
}

func (c *client) AccountTransactions(accountID int64, options ...option) (
	string, []Transaction, error) {
	urlStr := fmt.Sprintf("%s/accounts/%d/transactions/", c.url, accountID)
	url, err := url.Parse(urlStr)
	if err != nil {
		return "", nil, err
	}

	for _, op := range options {
		if err := op(url); err != nil {
			return "", nil, err
		}
	}

	req, err := http.NewRequest("GET", url.String(), nil)
	if err != nil {
		return "", nil, err
	}
	req.SetBasicAuth(c.user, c.pass)
	req.Header.Set("Accept", "application/json")
	next, payload, err := c.do(req)
	if err != nil {
		return "", nil, err
	}
	txns := []Transaction{}
	if err := json.Unmarshal(payload, &txns); err != nil {
		return "", nil, err
	}
	return next, txns, nil
}

func (c *client) CreateTransactionIDs(n int) ([]int64, error) {
	urlStr := fmt.Sprintf("%s/transactions/", c.url)

	var postBody bytes.Buffer
	if err := json.NewEncoder(&postBody).Encode(struct {
		N int `json:"n"`
	}{
		N: n,
	}); err != nil {
		return nil, err
	}

	req, err := http.NewRequest("POST", urlStr, &postBody)
	if err != nil {
		return nil, err
	}
	req.SetBasicAuth(c.user, c.pass)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")

	_, payload, err := c.do(req)
	if err != nil {
		return nil, err
	}
	txns := make([]Transaction, 0, n)
	if err := json.Unmarshal(payload, &txns); err != nil {
		return nil, err
	}

	txIDs := make([]int64, n)
	for i, tx := range txns {
		txIDs[i] = tx.ID
	}

	return txIDs, nil
}

func (c *client) Transaction(id int64) (Transaction, error) {
	urlStr := fmt.Sprintf("%s/transactions/%d", c.url, id)
	req, err := http.NewRequest("GET", urlStr, nil)
	if err != nil {
		return Transaction{}, err
	}
	req.SetBasicAuth(c.user, c.pass)
	req.Header.Set("Accept", "application/json")
	_, payload, err := c.do(req)
	if err != nil {
		return Transaction{}, err
	}

	txns := make([]Transaction, 0, 1)
	if err := json.Unmarshal(payload, &txns); err != nil {
		return Transaction{}, err
	}

	return txns[0], nil
}

func (c *client) Transfer(txID, fromAccountID, toAccountID, value int64) error {

	urlStr := fmt.Sprintf("%s/transactions/", c.url)

	transferReq := struct {
		ID            int64 `json:"id"`
		FromAccountID int64 `json:"fromAccountID"`
		ToAccountID   int64 `json:"toAccountID"`
		Value         int64 `json:"value"`
	}{
		ID:            txID,
		FromAccountID: fromAccountID,
		ToAccountID:   toAccountID,
		Value:         value,
	}

	var putBody bytes.Buffer
	if err := json.NewEncoder(&putBody).Encode(transferReq); err != nil {
		return err
	}

	req, err := http.NewRequest("PUT", urlStr, &putBody)
	if err != nil {
		return err
	}
	req.SetBasicAuth(c.user, c.pass)
	req.Header.Set("Content-Type", "application/json")

	if _, _, err := c.do(req); err != nil {

		switch err.Error() {
		case "insufficient funds":
			return ErrInsufficientFunds
		}
		return err
	}
	return nil
}

func (c *client) Debit(txID, fromAccountID int64, toAddress string,
	value int64) error {

	urlStr := fmt.Sprintf("%s/transactions/", c.url)

	var body bytes.Buffer
	if err := json.NewEncoder(&body).Encode(struct {
		TxID          int64  `json:"id"`
		FromAccountID int64  `json:"fromAccountID"`
		ToAddress     string `json:"toAddress"`
		Value         int64  `json:"value"`
	}{
		TxID:          txID,
		FromAccountID: fromAccountID,
		ToAddress:     toAddress,
		Value:         value,
	}); err != nil {
		return err
	}

	req, err := http.NewRequest("PUT", urlStr, &body)
	if err != nil {
		return err
	}
	req.SetBasicAuth(c.user, c.pass)
	req.Header.Set("Content-Type", "application/json")

	if _, _, err := c.do(req); err != nil {
		return err
	}
	return nil
}

func (c *client) CreateHook(url string) error {

	urlStr := fmt.Sprintf("%s/hooks/", c.url)

	hookReq := struct {
		URL string `json:"url"`
	}{url}

	body := &bytes.Buffer{}
	if err := json.NewEncoder(body).Encode(hookReq); err != nil {
		return err
	}

	req, err := http.NewRequest("POST", urlStr, body)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.SetBasicAuth(c.user, c.pass)

	if _, _, err := c.do(req); err != nil {
		switch err.Error() {
		case "hook exists":
			return ErrHookExists
		}
		return err
	}
	return nil
}

func (c *client) Hooks() ([]Hook, error) {
	urlStr := fmt.Sprintf("%s/hooks/", c.url)
	req, err := http.NewRequest("GET", urlStr, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.SetBasicAuth(c.user, c.pass)
	_, payload, err := c.do(req)
	if err != nil {
		return nil, err
	}
	hooks := []Hook{}
	return hooks, json.Unmarshal(payload, &hooks)
}

func (c *client) DeleteHook(url string) error {
	encodedURL := base64.URLEncoding.EncodeToString([]byte(url))
	urlStr := fmt.Sprintf("%s/hooks/%s", c.url, encodedURL)
	req, err := http.NewRequest("DELETE", urlStr, nil)
	if err != nil {
		return err
	}
	req.SetBasicAuth(c.user, c.pass)
	if _, _, err := c.do(req); err != nil {
		return err
	}
	return nil
}

func New(c *http.Client, url, user, pass string) Client {
	return &client{
		client: c,
		url:    url,
		user:   user,
		pass:   pass,
	}
}

type TransactionEvent struct {
	Transaction
	Status string `json:"status"`
}

func Unmarshal(r *http.Request) ([]TransactionEvent, error) {

	if r.Header.Get("Content-Type") != "application/json" {
		return nil, errors.New("incorrect content type")
	}
	defer r.Body.Close()

	obj := &object{}
	if err := json.NewDecoder(r.Body).Decode(obj); err != nil {
		return nil, err
	}

	switch obj.Type {
	case "transactions":
		var events []TransactionEvent
		return events, json.Unmarshal(obj.Payload, &events)
	default:
		return nil, fmt.Errorf("unknown object type %v", obj.Type)
	}
}
