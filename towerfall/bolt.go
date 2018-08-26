package towerfall

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/boltdb/bolt"
)

// Database is the persisting class
type Database struct {
	DB            *bolt.DB
	Server        *Server
	Tournaments   []*Tournament
	People        []*Person
	tournamentRef map[string]*Tournament
}

var (
	// TournamentKey defines the tournament buckets
	TournamentKey = []byte("tournaments")
	// PeopleKey defines the bucket of people
	PeopleKey = []byte("people")
	// MigrationKey defines the bucket of migration levels
	MigrationKey = []byte("migration")
)

var tournamentMutex = &sync.Mutex{}
var personMutex = &sync.Mutex{}

// Used to signal that a current tournament was found and that the
// scanner should stop iterating.
var ErrTournamentFound = errors.New("found")

// NewDatabase returns a new database object
func NewDatabase(fn string) (*Database, error) {
	// log.Printf("Opening database at '%s'", fn)
	bolt, err := bolt.Open(fn, 0600, &bolt.Options{Timeout: 1 * time.Second})
	if err != nil {
		log.Fatal(err)
	}

	db := &Database{DB: bolt}
	db.tournamentRef = make(map[string]*Tournament)
	db.LoadPeople()

	return db, nil
}

// LoadTournaments loads the tournaments from the database and into memory
func (d *Database) LoadTournaments() error {
	err := d.DB.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(TournamentKey)
		if b == nil {
			// If there is no bucket, bail silently.
			// This only really happens in tests.
			return nil
		}

		err := b.ForEach(func(k []byte, v []byte) error {
			t, err := LoadTournament(v, d)
			if err != nil {
				return err
			}

			tournamentMutex.Lock()
			d.Tournaments = append(d.Tournaments, t)
			d.tournamentRef[t.ID] = t
			tournamentMutex.Unlock()
			return nil
		})

		d.Tournaments = SortByScheduleDate(d.Tournaments)
		return err
	})

	return err
}

// SaveTournament stores the current state of the tournaments into the db
func (d *Database) SaveTournament(t *Tournament) error {
	ret := d.DB.Update(func(tx *bolt.Tx) error {
		b, err := tx.CreateBucketIfNotExists(TournamentKey)
		if err != nil {
			return err
		}

		json, _ := t.JSON()
		err = b.Put([]byte(t.ID), json)
		if err != nil {
			log.Fatal(err)
		}

		return nil
	})

	// If the tournament isn't already in the cache, we should add it
	found := false
	for _, ct := range d.Tournaments {
		if ct.ID == t.ID {
			found = true
			break
		}
	}
	if !found {
		log.Printf("Adding new tournament %s into the memory cache", t.ID)
		d.Tournaments = append(d.Tournaments, t)
		d.tournamentRef[t.ID] = t
	}

	go d.Server.SendWebsocketUpdate("tournament", t)
	return ret
}

// OverwriteTournament takes a new foreign Tournament{} object and replaces
// the one with the same ID with that one.
//
// Used from the EditHandler()
func (d *Database) OverwriteTournament(t *Tournament) error {
	ret := d.DB.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(TournamentKey)

		json, err := t.JSON()
		if err != nil {
			log.Fatal(err)
		}

		err = b.Put([]byte(t.ID), json)
		if err != nil {
			log.Fatal(err)
		}

		// Replace the tournament in the in-memory list
		for j := 0; j < len(d.Tournaments); j++ {
			ot := d.Tournaments[j]
			if t.ID == ot.ID {
				d.Tournaments = d.Tournaments[:j]
				d.Tournaments = append(d.Tournaments, t)
				d.Tournaments = append(d.Tournaments, d.Tournaments[j+1:]...)
				break
			}
		}
		// And lastly the reference
		d.tournamentRef[t.ID] = t

		return nil
	})

	return ret
}

// SavePerson stores a person into the DB
func (d *Database) SavePerson(p *Person) error {
	err := d.DB.Update(func(tx *bolt.Tx) error {
		b, err := tx.CreateBucketIfNotExists(PeopleKey)
		if err != nil {
			return err
		}

		json, _ := p.JSON()
		err = b.Put([]byte(p.ID), json)
		if err != nil {
			log.Fatal(err)
		}

		return nil
	})

	if err == nil {
		d.LoadPeople()
	}

	return err
}

// GetPerson gets a Person{} from the DB
func (d *Database) GetPerson(id string) (*Person, error) {
	tx, err := d.DB.Begin(false)
	if err != nil {
		fmt.Println(err)
		return nil, err
	}
	defer tx.Rollback()

	b := tx.Bucket(PeopleKey)
	if b == nil {
		return nil, errors.New("database not initialized")
	}
	out := b.Get([]byte(id))
	if out == nil {
		return &Person{}, errors.New("user not found")
	}
	p := &Person{}
	_ = json.Unmarshal(out, p)
	return p, nil
}

// GetSafePerson gets a Person{} from the DB, while being absolutely
// sure there will be no error.
//
// This is only for hardcoded cases where error handling is just pointless.
func (d *Database) GetSafePerson(id string) *Person {
	p, _ := d.GetPerson(id)
	return p
}

// DisablePerson disables or re-enables a person
func (d *Database) DisablePerson(id string) error {
	p, err := d.GetPerson(id)
	if err != nil {
		return err
	}

	p.Disabled = !p.Disabled
	d.SavePerson(p)

	return nil
}

// LoadPeople loads the people from the database and into memory
func (d *Database) LoadPeople() error {
	d.People = make([]*Person, 0)
	err := d.DB.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(PeopleKey)
		if b == nil {
			return nil // Test setup - no profiles
		}

		err := b.ForEach(func(k []byte, v []byte) error {
			p, err := LoadPerson(v)

			// If the player is disabled, just skip them
			if err == ErrPlayerDisabled {
				return nil
			}

			if err != nil {
				return err
			}

			personMutex.Lock()
			d.People = append(d.People, p)
			personMutex.Unlock()
			return nil
		})
		return err
	})

	return err
}

// GetCurrentTournament gets the currently running tournament.
//
// Returns the first matching one, so if there are multiple they will
// be shadowed.
func (d *Database) GetCurrentTournament() (*Tournament, error) {
	for _, t := range SortByScheduleDate(d.Tournaments) {
		if t.IsRunning() {
			return t, nil
		}
	}
	return &Tournament{}, errors.New("no tournament is running")
}

// ClearTestTournaments deletes any tournament that doesn't begin with "DrunkenFall"
func (d *Database) ClearTestTournaments() error {
	err := d.DB.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(TournamentKey)

		err := b.ForEach(func(k []byte, v []byte) error {
			t, err := LoadTournament(v, d)
			if err != nil {
				return err
			}

			if !strings.HasPrefix(t.Name, "DrunkenFall") {
				log.Print("Deleting ", t.ID)
				err := b.Delete([]byte(t.ID))
				if err != nil {
					return err
				}
			}
			return nil

		})
		return err
	})

	d.Tournaments = make([]*Tournament, 0)
	err = d.LoadTournaments()
	if err != nil {
		return err
	}

	d.Server.SendWebsocketUpdate("all", d.asMap())

	return err
}

func (d *Database) asMap() map[string]*Tournament {
	tournamentMutex.Lock()
	out := make(map[string]*Tournament)
	for _, t := range d.Tournaments {
		out[t.ID] = t
	}
	tournamentMutex.Unlock()
	return out
}

// Close closes the database
func (d *Database) Close() error {
	return d.DB.Close()
}

// ByScheduleDate is a sort.Interface that sorts tournaments according
// to when they were scheduled.
type ByScheduleDate []*Tournament

func (s ByScheduleDate) Len() int {
	return len(s)
}
func (s ByScheduleDate) Swap(i, j int) {
	s[i], s[j] = s[j], s[i]

}
func (s ByScheduleDate) Less(i, j int) bool {
	return s[i].Scheduled.Before(s[j].Scheduled)
}

// SortByScheduleDate returns a list in order of schedule date
func SortByScheduleDate(ps []*Tournament) []*Tournament {
	sort.Sort(ByScheduleDate(ps))
	return ps
}