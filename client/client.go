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
	// MainNetURL is the URL that should be supplied to New() in order to
	// connect to RTWire's mainnet network.
	MainNetURL = "https://api.rtwire.com/v1/mainnet"

	// TestNet3URL is the URL that should be supplied to New() in order to
	// connect to RTWire's testnet3 network.
	TestNet3URL = "https://api/rtwire.com/v1/testnet3"
)

var (
	// ErrTxIDUsed is returned from Transfer or Debit if the transaction ID has
	// already been used.
	ErrTxIDUsed = errors.New("TxID used")

	// ErrInsufficientFunds is returned from Transfer or Debit if the account
	// sending the funds has insufficient satoshi.
	ErrInsufficientFunds = errors.New("insufficient funds")

	// ErrHookExists is returned if a web hook has already been registered.
	ErrHookExists = errors.New("hook exists")
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

// Limit limits the maximum number of results returned from both the
// AccountTransactions and Account endpoints.
func Limit(limit int) option {
	return func(u *url.URL) error {
		return setQueryValue(u, "limit", strconv.Itoa(limit))
	}
}

// Next takes the cursor value of a previous call to AccountTransactions, or
// Accounts in order to page through the next set of results.
func Next(next string) option {
	return func(u *url.URL) error {
		return setQueryValue(u, "next", next)
	}
}

// Pending is an option used with AccountTransactions to return only the
// transactions that have been detected by RTWire but have not yet been credited
// to an account.
func Pending() option {
	return func(u *url.URL) error {
		return setQueryValue(u, "status", "pending")
	}
}

// Client allows Go applications to connect to the RTWire HTTP endpoints. See
// https://rtwire.com/docs for more information.
type Client interface {
	CreateAccount() (Account, error)
	Account(accountID int64) (Account, error)
	Accounts(options ...option) (string, []Account, error)

	CreateAddress(accountID int64) (string, error)

	CreateTransactionIDs(int) ([]int64, error)
	Transaction(txID int64) (Transaction, error)
	AccountTransactions(accountID int64, options ...option) (
		string, []Transaction, error)

	Transfer(txID, fromAccountID, toAccountID, value int64) error
	Debit(txID, fromAccountID int64, toAddress string, value int64) error

	Fees() ([]Fee, error)

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

// Account represents an RTWire account. See https://rtwire.com/docs#accounts
// for more information.
type Account struct {
	ID      int64 `json:"id"`
	Balance int64 `json:"balance"`
}

// Transaction represents a RTWire transaction. See
// https://rtwire.com/docs#transactions for more information.
type Transaction struct {
	ID int64 `json:"id"`

	Type string `json:"type"`

	FromAccountID int64 `json:"fromAccountID"`
	ToAccountID   int64 `json:"toAccountID"`

	FromAccountBalance int64 `json:"fromAccountBalance"`
	ToAccountBalance   int64 `json:"toAccountBalance"`

	FromAccountTxID int64 `json:"fromAccountTxID"`
	ToAccountTxID   int64 `json:"toAccountTxID"`

	Value   int64     `json:"value"`
	Created time.Time `json:"created"`

	TxHashes   []string `json:"txHashes"`
	TxOutIndex int64    `json:"txOutIndex"`
}

type Fee struct {
	FeePerByte  int64 `json:"feePerByte"`
	BlockHeight int64 `json:"blockHeight"`
}

// Hook represents an RTWire hook. See https://rtwire.com/docs#hooks for more
// information.
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

// CreateAccount creates a new account. See
// https://rtwire.com/docs#post-accounts for more information.
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

// Account returns the account specified by id. See
// https://rtwire.com/docs#get-account for more information.
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

// Accounts returns a cursor for the next set of accouts, a list of accounts and// any errors which may have occured. Next() can be used to cursor through the
// next set of accounts by passing in the previous cursor value. Limit() can be
// used to limit the number of accounts that are returned in one call. See
// https://rtwire.com/docs#get-accounts for more information.
func (c *client) Accounts(options ...option) (string, []Account, error) {

	urlStr := fmt.Sprintf("%s/accounts/", c.url)
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

	accs, err := accountsFromPayload(payload)
	if err != nil {
		return "", nil, err
	}
	return next, accs, err
}

// CreateAddress creates a public key hash address associated with accountID.
// Any bitcoins transfered to that address will credit the account associated
// with accountID. See https://rtwire.com/docs#post-addresses for more
// information.
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

// AccountTransactions list all the transactions involving accountID. As there
// may be many transactions a paging system is used. The first returned value
// is a cursor for the next set of results. Next() with the previous cursor can
// be used as an option to retrieve the next set of results. Limit() can be used
// to determine how many transactions are returned with one call. The Pending()
// option can be used to list all pending transactions for the specified
// account. See https://rtwire.com/docs#get-account-transactions for more
// information.
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

// CreateTransactionIDs creates transaction ids that can be used to transfer
// and debit satoshi from RTWire accounts. A transaction ID can only be used
// successfully once. Allowing clients to create transaction IDs prior to
// creating transactions through debits and transfers ensures that transactions
// can be made idempotent. See https://rtwire.com/docs#put-transactions for more
// information.
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

// Transaction returns transaction information for transaction id. See
// https://rtwire.com/docs#get-transaction for more information.
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

// Transfer transfers value satoshi from fromAccountID to toAccountID. A
// transaction ID, txID can be obtained from CreateTransactionIDs. See
// https://rtwire.com/docs#put-transactions for more information.
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

// Debit debits value satoshi from fromAccountID to a public key hash address
// toAddress. A transaction ID, txID, can be obtained from CreateTransactionIDs.
// See https://rtwire.com/docs#put-transactions for more information.
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

// Fees returns the current estimated miner fees. This gives an idea of how much
// a debit will cost in miner fees.See https://rtwire.com/docs#get-fees for more
// information.
func (c *client) Fees() ([]Fee, error) {
	req, err := http.NewRequest("GET", c.url+"/fees/", nil)
	if err != nil {
		return nil, err
	}
	req.SetBasicAuth(c.user, c.pass)
	req.Header.Set("Accept", "application/json")
	_, payload, err := c.do(req)
	if err != nil {
		return nil, err
	}

	fees := []Fee{}
	return fees, json.Unmarshal(payload, &fees)
}

// CreateHook creates a web hook. Every time a transaction is potentially
// credited to an account url will be called. Note that url may be called
// several times for the same transaction. See
// https://rtwire.com/docs#post-hooks for more information.
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

// Hooks lists the registered web hooks. See https://rtwire.com/docs#get-hooks
// for more information.
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

// DeleteHook deletes a web hook with the specified url. See
// https://rtwire.com/docs#delete-hook for more information.
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

// New creates a new client. URL can either be MainNetURL or TestNet3URL to
// connect to their respective RTWire endpoints. User and pass represent
// credentials that can be found at https://console.rtwire.com/.
func New(c *http.Client, url, user, pass string) Client {
	return &client{
		client: c,
		url:    url,
		user:   user,
		pass:   pass,
	}
}

// TransactionEvent represents a RTWire transaction event generated by a
// registered hook. The transaction status can either be blank or 'pending'. If
// it is pending then RTWire has registered the transaction but is not confident
// enough in it to release it to the account.
type TransactionEvent struct {
	Transaction
	Status string `json:"status"`
}

// Unmarshal takes an http.Request that has been generated by an RTWire hook
// event and returns a TransactionEvent. See https://rtwire.com/docs#hook-event
// for more information.
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
