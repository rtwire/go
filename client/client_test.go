package client_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/rtwire/go/client"
	"github.com/rtwire/mock/service"
)

func TestAccount(t *testing.T) {

	server := httptest.NewServer(service.New())
	defer server.Close()

	url := fmt.Sprintf("%s/v1/mainnet", server.URL)
	client := client.New(http.DefaultClient, url, "user", "pass")

	accOne, err := client.CreateAccount()
	if err != nil {
		t.Fatal(err)
	}

	if accOne.ID == 0 {
		t.Fatal("account ID not set")
	}

	accTwo, err := client.Account(accOne.ID)
	if err != nil {
		t.Fatal(err)
	}
	if accOne.ID != accTwo.ID {
		t.Fatal("incorrect account id")
	}

	accs, err := client.Accounts()
	if err != nil {
		t.Fatal(err)
	}
	if len(accs) != 1 {
		t.Fatal("expected one account", len(accs))
	}

	if accs[0].ID != accOne.ID {
		t.Fatal("incorrect account ID")
	}
}

func TestCreateAddress(t *testing.T) {

	server := httptest.NewServer(service.New())
	defer server.Close()

	url := fmt.Sprintf("%s/v1/mainnet", server.URL)
	client := client.New(http.DefaultClient, url, "user", "pass")

	acc, err := client.CreateAccount()
	if err != nil {
		t.Fatal(err)
	}

	addr, err := client.CreateAddress(acc.ID)
	if err != nil {
		t.Fatal(err)
	}

	if addr == "" {
		t.Fatal("expected address")
	}
}

func TestTransfer(t *testing.T) {

	server := httptest.NewServer(service.New())
	defer server.Close()

	url := fmt.Sprintf("%s/v1/mainnet", server.URL)
	cl := client.New(http.DefaultClient, url, "user", "pass")

	// Create a sender account.
	accOne, err := cl.CreateAccount()
	if err != nil {
		t.Fatal(err)
	}

	// Create an address for the sender account.
	accOneAddr, err := cl.CreateAddress(accOne.ID)
	if err != nil {
		t.Fatal(err)
	}

	// Populate the account with funds.
	addrURL := fmt.Sprintf("%s/addresses/%s", url, accOneAddr)
	var addrPayload bytes.Buffer
	if err := json.NewEncoder(&addrPayload).Encode(struct {
		Value int64 `json:"value"`
	}{
		Value: 10,
	}); err != nil {
		t.Fatal(err)
	}

	req, err := http.NewRequest("POST", addrURL, &addrPayload)
	if err != nil {
		t.Fatal(err)
	}
	req.SetBasicAuth("user", "pass")
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}

	if resp.StatusCode != http.StatusOK {
		t.Fatal("expected status ok", resp.StatusCode)
	}

	// Create a recipient account.
	accTwo, err := cl.CreateAccount()
	if err != nil {
		t.Fatal(err)
	}

	// Create a txID.
	txIDs, err := cl.CreateTransactionIDs(1)
	if err != nil {
		t.Fatal(err)
	}

	// Transfer funds from sender to recipient account.
	if err := cl.Transfer(txIDs[0], accOne.ID, accTwo.ID, 5); err != nil {
		t.Fatal(err)
	}

	// Check transfer occured.
	accOne, err = cl.Account(accOne.ID)
	if err != nil {
		t.Fatal(err)
	}

	if accOne.Balance != 5 {
		t.Fatal("incorrect balance")
	}

	accTwo, err = cl.Account(accTwo.ID)
	if err != nil {
		t.Fatal(err)
	}

	if accTwo.Balance != 5 {
		t.Fatal("incorrect balance")
	}

	// Check transaction is registered.
	tx, err := cl.Transaction(txIDs[0])
	if err != nil {
		t.Fatal(err)
	}

	if tx.ID != txIDs[0] {
		t.Fatal("incorrect tx id")
	}

	// Check each account has the transaction registered.
	_, txns, err := cl.AccountTransactions(accOne.ID)
	if err != nil {
		t.Fatal(err)
	}

	if len(txns) != 2 {
		t.Fatalf("expected two transactions %+v", txns)
	}

	_, txns, err = cl.AccountTransactions(accTwo.ID)
	if err != nil {
		t.Fatal(err)
	}

	if len(txns) != 1 {
		t.Fatal("expected one transaction", txns)
	}
}

func TestHooks(t *testing.T) {

	server := httptest.NewServer(service.New())
	defer server.Close()

	type eventsMsg struct {
		events []client.TransactionEvent
		err    error
	}

	eventsChan := make(chan eventsMsg)
	clientServer := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			events, err := client.Unmarshal(r)
			eventsChan <- eventsMsg{
				events: events,
				err:    err,
			}
		}))
	defer clientServer.Close()

	url := fmt.Sprintf("%s/v1/mainnet", server.URL)
	cl := client.New(http.DefaultClient, url, "user", "pass")

	hookURL := clientServer.URL
	if err := cl.CreateHook(hookURL); err != nil {
		t.Fatal(err)
	}

	if err := cl.CreateHook(hookURL); err == nil {
		t.Fatal("expected duplicate error")
	} else if err != client.ErrHookExists {
		t.Fatal(err)
	}

	hooks, err := cl.Hooks()
	if err != nil {
		t.Fatal(err)
	}

	if hooks[0].URL != hookURL {
		t.Fatal("incorrect hook url")
	}

	// Check unmarshall works by creating an account with address and sending
	// it some money.
	acc, err := cl.CreateAccount()
	if err != nil {
		t.Fatal(err)
	}
	addr, err := cl.CreateAddress(acc.ID)
	if err != nil {
		t.Fatal(err)
	}

	addrURL := fmt.Sprintf("%s/addresses/%s", url, addr)
	var addrPayload bytes.Buffer
	if err := json.NewEncoder(&addrPayload).Encode(struct {
		Value int64 `json:"value"`
	}{
		Value: 10,
	}); err != nil {
		t.Fatal(err)
	}

	req, err := http.NewRequest("POST", addrURL, &addrPayload)
	if err != nil {
		t.Fatal(err)
	}
	req.SetBasicAuth("user", "pass")
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}

	if resp.StatusCode != http.StatusOK {
		t.Fatal("expected status ok", resp.StatusCode)
	}

	// Fetch the hook event result and make sure it is populated.
	msg := <-eventsChan
	if msg.err != nil {
		t.Fatal(msg.err)
	}

	if len(msg.events) != 1 {
		t.Fatal("expected one event")
	}

	if msg.events[0].ID == 0 {
		t.Fatal("tx ID not set")
	}

	if msg.events[0].Type != client.Credit {
		t.Fatal("expected credit tx type")
	}

	if err := cl.DeleteHook(hookURL); err != nil {
		t.Fatal(err)
	}

	hooks, err = cl.Hooks()
	if err != nil {
		t.Fatal(err)
	}

	if len(hooks) != 0 {
		t.Fatal("expected no hooks")
	}
}

func TestDebit(t *testing.T) {

	server := httptest.NewServer(service.New())
	defer server.Close()

	url := fmt.Sprintf("%s/v1/mainnet", server.URL)
	client := client.New(http.DefaultClient, url, "user", "pass")

	// Create a sender account.
	acc, err := client.CreateAccount()
	if err != nil {
		t.Fatal(err)
	}

	// Create an address for the sender account.
	accAddr, err := client.CreateAddress(acc.ID)
	if err != nil {
		t.Fatal(err)
	}

	// Populate the account with funds.
	addrURL := fmt.Sprintf("%s/addresses/%s", url, accAddr)
	var addrPayload bytes.Buffer
	if err := json.NewEncoder(&addrPayload).Encode(struct {
		Value int64 `json:"value"`
	}{
		Value: 10,
	}); err != nil {
		t.Fatal(err)
	}

	req, err := http.NewRequest("POST", addrURL, &addrPayload)
	if err != nil {
		t.Fatal(err)
	}
	req.SetBasicAuth("user", "pass")
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}

	if resp.StatusCode != http.StatusOK {
		t.Fatal("expected status ok", resp.StatusCode)
	}

	// Create a txID.
	txIDs, err := client.CreateTransactionIDs(1)
	if err != nil {
		t.Fatal(err)
	}

	// Debit funds.
	const debitAddr = "12aXxEWgTYZgAiGC81Tqu1cSiDUSy3embt"
	if err := client.Debit(txIDs[0], acc.ID, debitAddr, 5); err != nil {
		t.Fatal(err)
	}
}
