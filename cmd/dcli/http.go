package main

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/cookiejar"
	"os"
	"path/filepath"
	"strings"

	"github.com/demisto/download/domain"
)

const (
	// xsrfTokenKey ...
	xsrfTokenKey = "X-XSRF-TOKEN"
	// xsrfCookieKey ...
	xsrfCookieKey = "XSRF-TOKEN"
)

type credentials struct {
	User     string `json:"user"`
	Password string `json:"password"`
}

// Client implements a client for the Demisto download server
type Client struct {
	*http.Client
	credentials *credentials
	username    string
	password    string
	server      string
	token       string
}

// New client that does not do anything yet before the login
func New(username, password, server string, insecure bool) (*Client, error) {
	if username == "" || password == "" || server == "" {
		return nil, errors.New("Please provide all the parameters")
	}
	if !strings.HasSuffix(server, "/") {
		server += "/"
	}
	cookieJar, _ := cookiejar.New(nil)
	c := &Client{Client: &http.Client{Jar: cookieJar}, credentials: &credentials{User: username, Password: password}, server: server}
	if insecure {
		c.Client.Transport = &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}
	}
	c.Jar = cookieJar
	req, err := http.NewRequest("GET", server, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	for _, element := range resp.Cookies() {
		if element.Name == xsrfCookieKey {
			c.token = element.Value
		}
	}
	return c, nil
}

// handleError will handle responses with status code different from success
func (c *Client) handleError(resp *http.Response) error {
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("Unexpected status code: %d (%s)", resp.StatusCode, http.StatusText(resp.StatusCode))
	}
	return nil
}

func (c *Client) req(method, path, contentType string, body io.Reader, result interface{}) error {
	req, err := http.NewRequest(method, c.server+path, body)
	if err != nil {
		return err
	}
	req.Header.Add("Accept", "application/json")
	if contentType == "" {
		req.Header.Add("Content-type", "application/json")
	} else {
		req.Header.Add("Content-type", contentType)
	}
	req.Header.Add(xsrfTokenKey, c.token)
	resp, err := c.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if err = c.handleError(resp); err != nil {
		return err
	}
	if result != nil {
		switch result := result.(type) {
		// Should we just dump the response body
		case io.Writer:
			if _, err = io.Copy(result, resp.Body); err != nil {
				return err
			}
		default:
			if err = json.NewDecoder(resp.Body).Decode(result); err != nil {
				return err
			}
		}
	}
	return nil
}

// Login to the Demisto download server, and returns statues code
func (c *Client) Login() (*domain.User, error) {
	creds, err := json.Marshal(c.credentials)
	if err != nil {
		return nil, err
	}
	u := &domain.User{}
	err = c.req("POST", "login", "", bytes.NewBuffer(creds), u)
	return u, err
}

// Logout from the Demisto server
func (c *Client) Logout() error {
	return c.req("POST", "logout", "", nil, nil)
}

func (c *Client) Tokens() (tokens []domain.Token, err error) {
	err = c.req("GET", "token", "", nil, &tokens)
	return
}

func (c *Client) DownloadLog() (l []domain.DownloadLog, err error) {
	err = c.req("GET", "log", "", nil, &l)
	return
}

func (c *Client) ListDownloads() (d []domain.Download, err error) {
	err = c.req("GET", "list-downloads", "", nil, &d)
	return
}

type userDetails struct {
	Username string          `json:"username"`
	Password string          `json:"password"`
	Email    string          `json:"email"`
	Name     string          `json:"name"`
	Type     domain.UserType `json:"type"`
	Token    string          `json:"token"`
}

func (c *Client) SetUser(u *userDetails) (*domain.User, error) {
	b, err := json.Marshal(u)
	if err != nil {
		return nil, err
	}
	res := &domain.User{}
	err = c.req("POST", "user", "", bytes.NewBuffer(b), res)
	return res, err
}

// Upload adds a version to the download server
func (c *Client) Upload(name, filePath string) error {
	b := &bytes.Buffer{}
	writer := multipart.NewWriter(b)
	part, err := writer.CreateFormFile("file", filepath.Base(filePath))
	if err != nil {
		return err
	}
	f, err := os.Open(filePath)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = io.Copy(part, f)
	if err != nil {
		return err
	}
	namePart, err := writer.CreateFormField("name")
	if err != nil {
		return err
	}
	_, err = namePart.Write([]byte(name))
	if err != nil {
		return err
	}
	writer.Close()
	err = c.req("POST", "upload", writer.FormDataContentType(), b, nil)
	return err
}

type newTokens struct {
	Count     int `json:"count"`
	Downloads int `json:"downloads"`
}

func (c *Client) Generate(count, downloads int) (tokens []domain.Token, err error) {
	nt := &newTokens{Count: count, Downloads: downloads}
	b, err := json.Marshal(nt)
	if err != nil {
		return nil, err
	}
	err = c.req("POST", "tokens/generate", "", bytes.NewBuffer(b), &tokens)
	return
}

type newEmailToken struct {
	Email     string `json:"email"`
	Downloads int `json:"downloads"`
}

func (c *Client) GenerateForEmail(email string, downloads int) (token *domain.Token, err error) {
	nt := &newEmailToken{Email: email, Downloads: downloads}
	b, err := json.Marshal(nt)
	if err != nil {
		return nil, err
	}
	token = &domain.Token{}
	err = c.req("POST", "tokens/email", "", bytes.NewBuffer(b), &token)
	return
}

func (c *Client) Questions() (questions []domain.Quiz, err error) {
	err = c.req("GET", "quizall", "", nil, &questions)
	return
}
