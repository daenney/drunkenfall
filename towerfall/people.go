package towerfall

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// A Person is someone having a role in the tournament
type Person struct {
	ID              string   `json:"id"`
	Name            string   `json:"name"`
	Email           string   `json:"email"`
	Nick            string   `json:"nick"`
	ColorPreference []string `json:"color_preference"`
	FacebookID      string   `json:"facebook_id"`
	FacebookToken   string   `json:"facebook_token"`
	AvatarURL       string   `json:"avatar_url"`
	Userlevel       int      `json:"userlevel"`
}

// Credits represents the data structure needed to display the credits
type Credits struct {
	Executive     *Person   `json:"executive"`
	Producers     []*Person `json:"producers"`
	Players       []*Person `json:"players"`
	ArchersHarmed int       `json:"archers_harmed"`
}

// Userlevels. Designed so that we can insert new ones in between them.
const (
	PermissionProducer    = 100
	PermissionCommentator = 50
	PermissionJudge       = 30
	PermissionPlayer      = 10
)

var ErrFacebookAlreadyExists = errors.New("facebook user already exists")

type score map[int]string

// ScoreSummary is a collection of scores for a Person
type ScoreSummary struct {
	Totals      score
	Tournaments map[string]score
}

func (p *Person) String() string {
	return fmt.Sprintf(
		"<Player %s (%s)>",
		p.Name,
		p.Nick,
	)
}

// JSON returns the person as a JSON representation
func (p *Person) JSON() (out []byte, err error) {
	out, err = json.Marshal(p)
	return
}

// Score gets the score of the Person
//
// Returned as a map of the total score and an array of maps - one per
// tournament participated in.
func (p *Person) Score() *ScoreSummary {
	return nil
}

// CreateFromFacebook adds a new player via Facebook login
func CreateFromFacebook(s *Server, req *FacebookAuthResponse) *Person {
	p := &Person{
		ID:            req.ID,
		FacebookID:    req.ID,
		FacebookToken: req.Token,
		Name:          req.Name,
		Email:         req.Email,
		Userlevel:     PermissionPlayer,
	}

	p.PrefillNickname()

	s.DB.SavePerson(p)

	return p
}

// PrefillNickname makes a suggestion to the nick based on the person
// nolint: gocyclo
func (p *Person) PrefillNickname() {
	// TODO(thiderman): Move this into data files
	switch p.Name {
	case "Karl Johan Krantz":
		p.Nick = "Qrl-Astrid"
		p.Userlevel = PermissionProducer
	case "Ida Andreasson":
		p.Nick = "Blue"
		p.Userlevel = PermissionProducer
	case "Daniel Dala Tiderman":
		p.Nick = "Dala"
		p.Userlevel = PermissionProducer
	case "Lowe Thiderman":
		p.Nick = "thiderman"
		p.Userlevel = PermissionProducer
	case "Magnus Ulenius":
		p.Nick = "Goose"
		p.Userlevel = PermissionProducer
	case "Jonathan Gustafsson":
		p.Nick = "hest"
		p.Userlevel = PermissionProducer
	case "Barney Trotwell":
		p.Nick = "FrontierPsycho"
		p.Userlevel = PermissionProducer
	case "Yasa Akbulut":
		p.Nick = "yasa"
		p.Userlevel = PermissionProducer
	case "Mike Goeppner":
		p.Nick = "skolpadda"
		p.Userlevel = PermissionProducer

	// Commentators
	case "Daniel McHugh":
		p.Nick = "Radcliffe"
		p.Userlevel = PermissionCommentator

	// Judges
	case "Daniele Sluijters":
		p.Nick = "Daenney"
		p.Userlevel = PermissionJudge

	// Other lovelies
	case "Agnes Skoog":
		p.Nick = "#swagnes"
	case "Mattias Aali Ahlström":
		p.Nick = "Aali"
	case "Sam Wise Ingberg":
		p.Nick = "Samselott"
	}
}

// UpdatePerson updates a person from a JoinRequest
func (p *Person) UpdatePerson(r *SettingsPostRequest) {
	p.Name = r.Name
	p.Nick = r.Nick
	p.ColorPreference = []string{r.Color}
}

// PreferredColor returns the preferred color
func (p *Person) PreferredColor() string {
	return p.ColorPreference[0]
}

// Correct sets a name and a color if they are missing
//
// This happens if someone did not complete the registration, and we need to
// have something on their Person{} objects so that the app isn't overly
// confused.
func (p *Person) Correct() {
	if p.Nick == "" {
		// Pick the first name, just to have something
		p.Nick = strings.Split(p.Name, " ")[0]
		log.Printf("Corrected nick for %s", p)
	}

	if len(p.ColorPreference) == 0 {
		// Grab a random color and insert it into the preference.
		p.ColorPreference = append(p.ColorPreference, RandomColor(Colors))
		log.Printf("Corrected color for %s", p)
	}
}

// StoreCookies stores the cookies of the
func (p *Person) StoreCookies(w http.ResponseWriter, r *http.Request) error {
	c := &http.Cookie{
		Name:    "userlevel",
		Value:   strconv.Itoa(p.Userlevel),
		Path:    "/",
		Expires: time.Now().Add(30 * 24 * time.Hour), // Set to the same as CookieStore
	}
	http.SetCookie(w, c)

	session, _ := CookieStore.Get(r, "session")
	session.Values["user"] = p.ID
	session.Values["userlevel"] = p.Userlevel
	err := session.Save(r, w)
	if err != nil {
		log.Fatal(err)
	}

	log.Printf("Stored cookies for '%s'", p.Nick)
	return nil
}

// RemoveCookies ...
func (p *Person) RemoveCookies(w http.ResponseWriter, r *http.Request) error {
	c := &http.Cookie{
		Name:    "userlevel",
		Value:   "0",
		Path:    "/",
		Expires: time.Now(),
	}
	http.SetCookie(w, c)

	session, _ := CookieStore.Get(r, "session")
	delete(session.Values, "user")
	delete(session.Values, "userlevel")
	session.Save(r, w)

	return nil
}

// PersonFromSession returns the Person{} object attached to the session
func PersonFromSession(s *Server, r *http.Request) *Person {
	if r == nil {
		return nil
	}

	session, _ := CookieStore.Get(r, "session")
	id := session.Values["user"].(string)

	p, err := s.DB.GetPerson(id)
	if err != nil {
		log.Printf("Nonexisting session for '%s': %s", id, err)
		return nil
	}
	return p
}

// LoadPerson loads a person from the database
func LoadPerson(data []byte) (*Person, error) {
	p := &Person{}
	err := json.Unmarshal(data, p)

	if err != nil {
		log.Print(err)
		return p, err
	}

	return p, nil
}