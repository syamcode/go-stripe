package main

import (
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/syamcode/go-stripe/internal/cards"
	"github.com/syamcode/go-stripe/internal/encryption"
	"github.com/syamcode/go-stripe/internal/models"
	"github.com/syamcode/go-stripe/internal/urlsigner"
)

// renderError is a helper to log and render template errors
func (app *application) renderError(w http.ResponseWriter, r *http.Request, tmpl string, td *templateData, err error) {
	app.errorLog.Println(err)
}

func (app *application) Home(w http.ResponseWriter, r *http.Request) {
	if err := app.renderTemplate(w, r, "home", &templateData{}); err != nil {
		app.renderError(w, r, "home", &templateData{}, err)
	}
}

func (app *application) VirtualTerminal(w http.ResponseWriter, r *http.Request) {
	if err := app.renderTemplate(w, r, "terminal", &templateData{}); err != nil {
		app.renderError(w, r, "terminal", &templateData{}, err)
	}
}

type TransactionData struct {
	FirstName       string
	LastName        string
	Email           string
	PaymentIntentID string
	PaymentMethodID string
	PaymentAmount   int
	PaymentCurrency string
	WidgetID        int
	LastFour        string
	ExpiryMonth     int
	ExpiryYear      int
	BankReturnCode  string
}

// GetTransactionData gets transaction data from post and stripe
func (app *application) GetTransactionData(r *http.Request) (TransactionData, error) {
	var txnData TransactionData

	if err := r.ParseForm(); err != nil {
		return txnData, fmt.Errorf("error parsing form: %w", err)
	}

	// read posted data
	txnData = TransactionData{
		FirstName:       r.Form.Get("first_name"),
		LastName:        r.Form.Get("last_name"), 
		Email:           r.Form.Get("cardholder_email"),
		PaymentIntentID: r.Form.Get("payment_intent"),
		PaymentMethodID: r.Form.Get("payment_method"),
		PaymentCurrency: r.Form.Get("payment_currency"),
	}

	paymentAmount, _ := strconv.Atoi(r.Form.Get("payment_amount"))
	txnData.PaymentAmount = paymentAmount

	card := cards.Card{
		Secret: app.config.stripe.secret,
		Key:    app.config.stripe.key,
	}

	pi, err := card.RetrievePaymentIntent(txnData.PaymentIntentID)
	if err != nil {
		return txnData, fmt.Errorf("error getting payment intent: %w", err)
	}

	pm, err := card.GetPaymentMethod(txnData.PaymentMethodID)
	if err != nil {
		return txnData, fmt.Errorf("error getting payment method: %w", err)
	}

	txnData.LastFour = pm.Card.Last4
	txnData.ExpiryMonth = int(pm.Card.ExpMonth)
	txnData.ExpiryYear = int(pm.Card.ExpYear)
	txnData.BankReturnCode = pi.Charges.Data[0].ID

	return txnData, nil
}

func (app *application) VirtualTerminalPaymentSucceeded(w http.ResponseWriter, r *http.Request) {
	txnData, err := app.GetTransactionData(r)
	if err != nil {
		app.errorLog.Println(err)
		return
	}

	// create a new transaction
	txn := models.Transaction{
		Amount:              txnData.PaymentAmount,
		Currency:            txnData.PaymentCurrency,
		LastFour:            txnData.LastFour,
		ExpiryMonth:         txnData.ExpiryMonth,
		ExpiryYear:          txnData.ExpiryYear,
		BankReturnCode:      txnData.BankReturnCode,
		PaymentIntent:       txnData.PaymentIntentID,
		PaymentMethod:       txnData.PaymentMethodID,
		TransactionStatusID: 2,
	}

	if _, err = app.SaveTransaction(txn); err != nil {
		app.errorLog.Println(err)
		return
	}

	app.Session.Put(r.Context(), "receipt", txnData)
	http.Redirect(w, r, "/virtual-terminal-receipt", http.StatusSeeOther)
}

func (app *application) VirtualTerminalReceipt(w http.ResponseWriter, r *http.Request) {
	txn := app.Session.Get(r.Context(), "receipt").(TransactionData)
	data := map[string]interface{}{"txn": txn}
	app.Session.Remove(r.Context(), "receipt")
	
	if err := app.renderTemplate(w, r, "virtual-terminal-receipt", &templateData{
		Data: data,
	}); err != nil {
		app.renderError(w, r, "virtual-terminal-receipt", &templateData{Data: data}, err)
	}
}

func (app *application) PaymentSucceeded(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		app.errorLog.Println(err)
		return
	}

	widgetID, _ := strconv.Atoi(r.Form.Get("product_id"))

	txnData, err := app.GetTransactionData(r)
	if err != nil {
		app.errorLog.Println(err)
		return
	}

	// create a new customer
	customerID, err := app.SaveCustomer(txnData.FirstName, txnData.LastName, txnData.Email)
	if err != nil {
		app.errorLog.Println(err)
		return
	}

	// create a new transaction
	txn := models.Transaction{
		Amount:              txnData.PaymentAmount,
		Currency:            txnData.PaymentCurrency,
		LastFour:            txnData.LastFour,
		ExpiryMonth:         txnData.ExpiryMonth,
		ExpiryYear:          txnData.ExpiryYear,
		BankReturnCode:      txnData.BankReturnCode,
		PaymentIntent:       txnData.PaymentIntentID,
		PaymentMethod:       txnData.PaymentMethodID,
		TransactionStatusID: 2,
	}

	txnID, err := app.SaveTransaction(txn)
	if err != nil {
		app.errorLog.Println(err)
		return
	}

	// create a new order
	order := models.Order{
		WidgetID:      widgetID,
		TransactionID: txnID,
		CustomerID:    customerID,
		StatusID:      1,
		Quantity:      1,
		Amount:        txnData.PaymentAmount,
		CreatedAt:     time.Now(),
		UpdatedAt:     time.Now(),
	}

	if _, err = app.SaveOrder(order); err != nil {
		app.errorLog.Println(err)
		return
	}

	app.Session.Put(r.Context(), "receipt", txnData)
	http.Redirect(w, r, "/receipt", http.StatusSeeOther)
}

func (app *application) Receipt(w http.ResponseWriter, r *http.Request) {
	txn := app.Session.Get(r.Context(), "receipt").(TransactionData)
	data := map[string]interface{}{"txn": txn}
	app.Session.Remove(r.Context(), "receipt")
	
	if err := app.renderTemplate(w, r, "receipt", &templateData{
		Data: data,
	}); err != nil {
		app.renderError(w, r, "receipt", &templateData{Data: data}, err)
	}
}

// SaveCustomer saves a customer and returns id
func (app *application) SaveCustomer(firstName, lastName, email string) (int, error) {
	customer := models.Customer{
		FirstName: firstName,
		LastName:  lastName,
		Email:     email,
	}

	return app.DB.InsertCustomer(customer)
}

// SaveTransaction saves a transaction and returns id
func (app *application) SaveTransaction(txn models.Transaction) (int, error) {
	return app.DB.InsertTransaction(txn)
}

// SaveOrder saves an order and returns id
func (app *application) SaveOrder(order models.Order) (int, error) {
	return app.DB.InsertOrder(order)
}

// ChargeOnce displays the page to buy one widget
func (app *application) ChargeOnce(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	widgetID, _ := strconv.Atoi(id)

	widget, err := app.DB.GetWidget(widgetID)
	if err != nil {
		app.errorLog.Println(err)
		return
	}

	data := map[string]interface{}{"widget": widget}

	if err := app.renderTemplate(w, r, "buy-once", &templateData{
		Data: data,
	}, "stripe-js"); err != nil {
		app.renderError(w, r, "buy-once", &templateData{Data: data}, err)
	}
}

func (app *application) BronzePlan(w http.ResponseWriter, r *http.Request) {
	widget, err := app.DB.GetWidget(2)
	if err != nil {
		app.errorLog.Println(err)
		return
	}

	data := map[string]interface{}{"widget": widget}

	if err := app.renderTemplate(w, r, "bronze-plan", &templateData{
		Data: data,
	}); err != nil {
		app.renderError(w, r, "bronze-plan", &templateData{Data: data}, err)
	}
}

func (app *application) BronzePlanReceipt(w http.ResponseWriter, r *http.Request) {
	if err := app.renderTemplate(w, r, "bronze-plan-receipt", &templateData{}); err != nil {
		app.renderError(w, r, "bronze-plan-receipt", &templateData{}, err)
	}
}

//LoginPage displays the login page
func (app *application) LoginPage(w http.ResponseWriter, r *http.Request) {
	if err := app.renderTemplate(w, r, "login", &templateData{}); err != nil {
		app.renderError(w, r, "login", &templateData{}, err)
	}
}

func (app *application) PostLoginPage(w http.ResponseWriter, r *http.Request) {
	app.Session.RenewToken(r.Context())

	if err := r.ParseForm(); err != nil {
		app.errorLog.Println(err)
		return
	}

	email := r.Form.Get("email")
	password := r.Form.Get("password")

	id, err := app.DB.Authenticate(email, password)
	if err != nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	app.Session.Put(r.Context(), "userID", id)
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (app *application) Logout(w http.ResponseWriter, r *http.Request) {
	app.Session.Destroy(r.Context())
	app.Session.RenewToken(r.Context())
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

func (app *application) ForgotPassword(w http.ResponseWriter, r *http.Request) {
	if err := app.renderTemplate(w, r, "forgot-password", &templateData{}); err != nil {
		app.renderError(w, r, "forgot-password", &templateData{}, err)
	}
}

func (app *application) ResetPasswordPage(w http.ResponseWriter, r *http.Request) {
	email := r.URL.Query().Get("email")
	theURL := r.RequestURI
	testURL := fmt.Sprintf("%s%s", app.config.frontend, theURL)

	signer := urlsigner.Signer{
		SecretKey: []byte(app.config.secretkey),
	}

	if !signer.VerifyToken(testURL) {
		w.Write([]byte("Invalid url - tampering detected"))
		return
	}

	if signer.Expired(testURL, 60) {
		app.errorLog.Println("Link expired")
		return
	}

	encryptor := encryption.Encryption{
		Key: []byte(app.config.secretkey),
	}

	encryptedEmail, err := encryptor.Encrypt(email)
	if err != nil {
		app.errorLog.Println("Encryption failed")
		return
	}

	data := map[string]interface{}{"email": encryptedEmail}

	if err := app.renderTemplate(w, r, "reset-password", &templateData{
		Data: data,
	}); err != nil {
		app.renderError(w, r, "reset-password", &templateData{Data: data}, err)
	}
}

func (app *application) SalesPage(w http.ResponseWriter, r *http.Request) {
	if err := app.renderTemplate(w, r, "sales", &templateData{}); err != nil {
		app.renderError(w, r, "sales", &templateData{}, err)
	}
}

func (app *application) SubscriptionsPage(w http.ResponseWriter, r *http.Request) {
	if err := app.renderTemplate(w, r, "subscriptions", &templateData{}); err != nil {
		app.renderError(w, r, "subscriptions", &templateData{}, err)
	}
}

func (app *application) ViewSalePage(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	orderID, _ := strconv.Atoi(id)

	intMap := map[string]int{"id": orderID}

	if err := app.renderTemplate(w, r, "sale", &templateData{
		IntMap: intMap,
	}); err != nil {
		app.renderError(w, r, "sale", &templateData{IntMap: intMap}, err)
	}
}

func (app *application) ViewSubscriptionPage(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	orderID, _ := strconv.Atoi(id)

	intMap := map[string]int{"id": orderID}

	if err := app.renderTemplate(w, r, "subscription", &templateData{
		IntMap: intMap,
	}); err != nil {
		app.renderError(w, r, "subscription", &templateData{IntMap: intMap}, err)
	}
}

func (app *application) UsersPage(w http.ResponseWriter, r *http.Request) {
	if err := app.renderTemplate(w, r, "users", &templateData{}); err != nil {
		app.renderError(w, r, "users", &templateData{}, err)
	}
}

func (app *application) ViewUserPage(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	userID, _ := strconv.Atoi(id)

	intMap := map[string]int{"id": userID}

	if err := app.renderTemplate(w, r, "user", &templateData{
		IntMap: intMap,
	}); err != nil {
		app.renderError(w, r, "user", &templateData{IntMap: intMap}, err)
	}
}
