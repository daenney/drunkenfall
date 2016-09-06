package main

// Based on http://stackoverflow.com/a/36672164

import (
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"strings"

	"encoding/json"
	"github.com/gorilla/mux"
	"github.com/nu7hatch/gouuid"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/facebook"
	"os"
)

var (
	oauthConf        *oauth2.Config
	oauthStateString string
)

// FacebookAuthResponse is the data we get back on the initial auth
type FacebookAuthResponse struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Email string `json:"email"`
	Token string `json:"access_token"`
}

// JoinRequest is the request to join a tournament
type JoinRequest struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Nick  string `json:"nick"`
	Color string `json:"color"`
}

func init() {
	// You need to have these three env vars in your env for Facebook to work.
	// If you need access to them, talk to @thiderman.
	oauthConf = &oauth2.Config{
		ClientID:     os.Getenv("DF_FB_ID"),
		ClientSecret: os.Getenv("DF_FB_SECRET"),
		RedirectURL:  os.Getenv("DF_FB_CALLBACK"),
		Scopes:       []string{"public_profile", "email"},
		Endpoint:     facebook.Endpoint,
	}

	if os.Getenv("DF_FB_ID") != "" {
		log.Print("Facebook configuration loaded.")
	} else {
		log.Print("Facebook app configuration missing. Auth will not work.")
	}

	randomUUID, err := uuid.NewV4()
	if err != nil {
		log.Fatal(err)
	}

	oauthStateString = randomUUID.String()
}

const htmlIndex = `<html><body>
Log in with <a href="/facebook/login">facebook</a>
</body></html>
`

func (s *Server) handleMain(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(htmlIndex))
}

func (s *Server) handleFacebookLogin(w http.ResponseWriter, r *http.Request) {
	URL, err := url.Parse(oauthConf.Endpoint.AuthURL)
	if err != nil {
		log.Fatal("Parse: ", err)
	}
	parameters := url.Values{}
	parameters.Add("client_id", oauthConf.ClientID)
	parameters.Add("scope", strings.Join(oauthConf.Scopes, " "))
	parameters.Add("redirect_uri", oauthConf.RedirectURL)
	parameters.Add("response_type", "code")
	parameters.Add("state", oauthStateString)
	URL.RawQuery = parameters.Encode()
	url := URL.String()
	http.Redirect(w, r, url, http.StatusTemporaryRedirect)
}

func (s *Server) handleFacebookCallback(w http.ResponseWriter, r *http.Request) {
	state := r.FormValue("state")
	if state != oauthStateString {
		fmt.Printf("invalid oauth state, expected '%s', got '%s'\n", oauthStateString, state)
		http.Redirect(w, r, "/", http.StatusTemporaryRedirect)
		return
	}

	code := r.FormValue("code")

	token, err := oauthConf.Exchange(oauth2.NoContext, code)
	if err != nil {
		fmt.Printf("oauthConf.Exchange() failed with '%s'\n", err)
		http.Redirect(w, r, "/", http.StatusTemporaryRedirect)
		return
	}

	fbURL := "https://graph.facebook.com/me?fields=id,name,email&access_token="
	escToken := url.QueryEscape(token.AccessToken)
	resp, err := http.Get(fbURL + escToken)
	if err != nil {
		fmt.Printf("Get: %s\n", err)
		http.Redirect(w, r, "/", http.StatusTemporaryRedirect)
		return
	}
	defer resp.Body.Close()

	response, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		fmt.Printf("ReadAll: %s\n", err)
		http.Redirect(w, r, "/", http.StatusTemporaryRedirect)
		return
	}

	log.Printf("parseResponseBody: %s\n", string(response))
	req := &FacebookAuthResponse{
		Token: escToken,
	}

	err = json.Unmarshal(response, req)
	if err != nil {
		log.Print(err)
		return
	}

	print(req)

	p := CreateFromFacebook(s, req)
	v := url.Values{}
	v.Add("id", p.ID)
	v.Add("name", p.Name)
	v.Add("nick", p.Nick)

	lastURL := "/facebook/finalize?" + v.Encode()
	http.Redirect(w, r, lastURL, http.StatusTemporaryRedirect)
}

// handleFacebookRegister gets the POST from the user that saves the final
// state of Facebook registration
func (s *Server) handleFacebookRegister(w http.ResponseWriter, r *http.Request) {
	var req JoinRequest

	body, err := ioutil.ReadAll(r.Body)
	if err != nil {
		log.Print(err)
		return
	}

	err = json.Unmarshal(body, &req)
	if err != nil {
		log.Print(err)
		return
	}
	log.Print(req)

	p := s.DB.GetPerson(req.ID)
	p.UpdatePerson(&req)
	s.DB.SavePerson(p)
	log.Printf("%s has joined DrunkenFall!", req.Name)

	s.Redirect(w, "/")
}

// FacebookRouter builds the paths for Facebook handling
func (s *Server) FacebookRouter(r *mux.Router) {
	r.HandleFunc("/facebook/login", s.handleFacebookLogin)
	r.HandleFunc("/facebook/oauth2callback", s.handleFacebookCallback)
	r.HandleFunc("/facebook/register", s.handleFacebookRegister)
}
