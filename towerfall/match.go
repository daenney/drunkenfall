package towerfall

import (
	"fmt"
	"log"
	"math/rand"
	"strings"
	"time"

	"github.com/deckarep/golang-set"
	"github.com/gin-gonic/gin"
	"github.com/mitchellh/mapstructure"
	"github.com/pkg/errors"
	"go.uber.org/zap"
)

const (
	qualifying = "qualifying"
	playoff    = "playoff"
	final      = "final"
	special    = "kind"
)

var ErrPublishIncompleteMatch = errors.New("cannot publish match without four players")

// Match represents a game being played
//
// Match.ScoreOrder stores the index to the player in the relative position.
// E.g. if player 3 is in the lead, ScoreOrder[0] will be 2 (the index of
// player 3).
//
// Match.Commits is a list of one commit per round and represents the
// changeset of what happened in the match.
type Match struct {
	ID           uint `json:"id"`
	TournamentID uint
	Tournament   *Tournament   `json:"-" sql:"-"`
	Players      []Player      `json:"players"`
	Casters      []*Person     `json:"casters" sql:"-"`
	Kind         string        `json:"kind"`
	Index        int           `json:"index" sql:",notnull"`
	Length       int           `json:"length"`
	Pause        time.Duration `json:"pause"`
	Scheduled    time.Time     `json:"scheduled"`
	Started      time.Time     `json:"started"`
	Ended        time.Time     `json:"ended"`
	// KillOrder     []int         `json:"kill_order"`
	Rounds        []Round `json:"commits" sql:"-"`
	Commits       []Commit
	Messages      []Message `json:"messages"`
	Level         string    `json:"level"`
	Ruleset       string    `json:"ruleset"`
	currentRound  Round
	presentColors mapset.Set
}

// Round is a state commit for a round of a match
type Round struct {
	Kills     [][]int   `json:"kills"`
	Shots     []bool    `json:"shots"`
	Committed time.Time `json:"committed"`
	started   bool
}

// A Commit is a flat and SQL-friendly representation of a Round
type Commit struct {
	MatchID uint
	P1up    int
	P1down  int
	P1shot  bool
	P2up    int
	P2down  int
	P2shot  bool
	P3up    int
	P3down  int
	P3shot  bool
	P4up    int
	P4down  int
	P4shot  bool

	Committed time.Time
	started   bool
}

// NewMatch creates a new Match
func NewMatch(t *Tournament, kind string) *Match {
	index := len(t.Matches)
	m := Match{
		Index:      index,
		Kind:       kind,
		Tournament: t,
		Length:     t.Length,
		Pause:      time.Minute * 5,
		Rounds:     make([]Round, 0),
		currentRound: Round{
			Kills: [][]int{{0, 0}, {0, 0}, {0, 0}, {0, 0}},
			Shots: []bool{false, false, false, false},
		},
	}
	m.presentColors = mapset.NewSet()

	// Finals are longer, and so are playoffs
	if kind == final || kind == playoff {
		m.Length = t.FinalLength
	}

	err := t.db.AddMatch(t, &m)
	if err != nil {
		log.Fatal(err)
	}
	return &m
}

func (m *Match) String() string {
	var tempo string
	var name string

	if !m.IsStarted() {
		tempo = "not started"
	} else if m.IsEnded() {
		tempo = "ended"
	} else {
		tempo = "playing"
	}

	if m.Kind == final {
		name = "Final"
	} else {
		name = fmt.Sprintf("%s %d", strings.Title(m.Kind), m.Index+1)
	}

	names := make([]string, 0, len(m.Players))
	for _, p := range m.Players {
		names = append(names, p.Name())
	}

	return fmt.Sprintf(
		"<(%d)[%d] %s: %s - %s>",
		m.ID,
		m.Index,
		name,
		strings.Join(names, " / "),
		tempo,
	)
}

// Title returns a title string
func (m *Match) Title() string {
	l := 2
	if m.Kind == final {
		return "Final"
	} else if m.Kind == playoff {
		l = len(m.Tournament.Matches) - 3
	}

	out := fmt.Sprintf(
		"%s %d/%d",
		strings.Title(m.Kind),
		m.Index+1,
		l,
	)
	return out
}

// URL builds the URL to the match
func (m *Match) URL() string {
	out := fmt.Sprintf(
		"/%s/%d/",
		m.Tournament.Slug,
		m.Index,
	)
	return out
}

// realLevel returns the level name string that the game expects
func (m *Match) realLevel() string {
	return realNames[m.Level]
}

// AddPlayer adds a player to the match
func (m *Match) AddPlayer(p Player) error {
	if len(m.Players) == 4 {
		return errors.New("cannot add fifth player")
	}

	// Add all the previous players' colors.
	// This is to fix a bug with the presentColors map if the app has been
	// restarted. They cannot be added multiple times anyway.
	for _, p := range m.Players {
		m.presentColors.Add(p.Color)
	}

	p.Color = p.PreferredColor
	m.presentColors.Add(p.Color)

	// Also set the match pointer
	p.Match = m

	// Add the player into the databas, and reset the ID before doing so
	// so that repeat player objects (e.g. from going from tryout to
	// semi) get new objects
	// TODO(thiderman): This entire function should be refactored to
	// take a Person or a PlayerSummary instead
	p.ID = 0
	err := globalDB.AddPlayerToMatch(m, &p)
	if err != nil {
		return err
	}

	m.Players = append(m.Players, p)

	// If we're adding the fourth player, it's time to correct the conflicts
	if len(m.Players) == 4 && len(m.presentColors.ToSlice()) != 4 {
		if err := m.CorrectFuckingColorConflicts(); err != nil {
			log.Print("Correcting color conflicts failed")
			return err
		}
	}

	return nil
}

// UpdatePlayer updates a player for the given match
func (m *Match) UpdatePlayer(p Player) error {
	for i, o := range m.Players {
		if o.Name() == p.Name() {
			m.Players[i] = p
		}
	}
	return globalDB.UpdatePlayer(m, &p)
}

// CorrectFuckingColorConflicts corrects color conflicts :@ 😠
func (m *Match) CorrectFuckingColorConflicts() error {
	var player Player
	// Make a map of conflicting players keyed on the color
	pairs := make(map[string][]Person)
	for _, color := range m.presentColors.ToSlice() {
		c := color.(string)
		for _, p := range m.Players {
			if p.PreferredColor == c {
				pairs[c] = append(pairs[c], *p.getPerson())
			}
		}
	}

	// Loop over the colors and
	for _, pair := range pairs {
		// If there are two or more players in the group, there is a conflict and
		// they need to be corrected.
		if len(pair) >= 2 {
			// We want to sort them by score, so that we can let the player with the
			// highest score keep their color.
			ps, err := SortByColorConflicts(m, pair)
			if err != nil {
				return errors.WithStack(err)
			}

			for _, p := range ps[1:] {
				// For the players with lower scores, set their new colors
				new := RandomColor(AvailableColors(m))
				m.presentColors.Add(new)

				// FIXME(thiderman): There are better ways of updating the player
				for _, o := range m.Players {
					if p.PersonID == o.PersonID {
						player = o
						break
					}
				}

				player.Color = new

				// Since we are using the tournament level Player object, the compound
				// scores from all other matches are currently on it. Reset that.
				// p.Reset()

				if err := m.UpdatePlayer(player); err != nil {
					return errors.WithStack(err)
				}

				log.Printf("%s corrected from %s to %s", player.Nick, player.PreferredColor, new)
				// m.LogEvent(
				// 	"color_conflict",
				// 	"{nick} corrected from {preferred} to {new}", // Unfortunately we cannot reuse person from below..
				// 	"nick", player.Person.Nick,
				// 	"preferred", player.PreferredColor,
				// 	"new", new,
				// 	"person", player.Person)

			}
		}
	}
	return nil
}

// Commit applies the round actions to the state of the players
// TODO(thiderman): It should not be possible to commit to a non-started match
func (m *Match) Commit(round Round) {
	if round.IsShotUpdate() {
		// The only thing submitted was shots, just update the players
		for i, s := range round.Shots {
			if s {
				m.Players[i].AddShot()
			}
		}
	} else {
		// Apply normally
		for i, score := range round.Kills {
			kills := score[0]
			self := score[1]

			m.Players[i].AddKills(kills)
			if self == -1 {
				m.Players[i].AddSelf()
			}
			if self == -1 || kills == 3 || round.Shots[i] {
				m.Players[i].AddShot()
			}
		}
		m.Rounds = append(m.Rounds, round)
		// m.KillOrder = m.MakeKillOrder()
	}

	_ = m.Tournament.Persist()
}

// storeMessage stores a message on the match
func (m *Match) storeMessage(msg Message) error {
	m.Messages = append(m.Messages, msg)
	return globalDB.StoreMessage(m, &msg)
}

// handleMessage decides what to do with an incoming message
func (m *Match) handleMessage(msg Message) error {
	// Store the message. Do this before figuring out the type and even
	// if it would not be parsed.
	err := m.storeMessage(msg)
	if err != nil {
		return nil
	}

	switch msg.Type {
	case inKill:
		km := KillMessage{}
		err := mapstructure.Decode(msg.Data, &km)
		if err != nil {
			fmt.Println("Error: Could not decode mapstructure", err.Error())
		}

		return m.Kill(km)

	case inRoundStart:
		sr := StartRoundMessage{}
		err := mapstructure.Decode(msg.Data, &sr)
		if err != nil {
			fmt.Println("Error: Could not decode mapstructure", err.Error())
		}
		return m.StartRound(sr)

	case inRoundEnd:
		return m.EndRound()

	case inMatchStart:
		if m.IsStarted() && !m.IsEnded() {
			log.Print("Current match not ended; ignoring match_start")
			return nil
		}

		nm, err := m.Tournament.NextMatch()
		if err != nil {
			return err
		}

		return nm.Start(nil)

	case inMatchEnd:
		return m.End(nil)

	case inShot, inPickup:
		am := ArrowMessage{}
		err := mapstructure.Decode(msg.Data, &am)
		if err != nil {
			fmt.Println("Error: Could not decode mapstructure", err.Error())
		}
		return m.ArrowUpdate(am)

	case inShield:
		sm := ShieldMessage{}
		err := mapstructure.Decode(msg.Data, &sm)
		if err != nil {
			fmt.Println("Error: Could not decode mapstructure", err.Error())
		}
		return m.ShieldUpdate(sm)

	case inWings:
		wm := WingsMessage{}
		err := mapstructure.Decode(msg.Data, &wm)
		if err != nil {
			fmt.Println("Error: Could not decode mapstructure", err.Error())
		}
		return m.WingsUpdate(wm)

	case inOrbLava:
		lm := LavaOrbMessage{}
		err := mapstructure.Decode(msg.Data, &lm)
		if err != nil {
			fmt.Println("Error: Could not decode mapstructure", err.Error())
		}
		return m.LavaOrb(lm)

	default:
		log.Printf("Warning: Unknown message type '%s'", msg.Type)
	}

	return nil
}

// sendPlayerUpdate sends a status update for a single player
func (m *Match) sendPlayerUpdate(idx int) error {
	return m.Tournament.server.SendWebsocketUpdate(
		"player",
		PlayerStateUpdateMessage{
			m.Tournament.Slug,
			m.Index,
			idx,
			m.Players[idx].State,
		},
	)
}

// EndRound is similar to Commit, but does not alter the score other
// than to manage shots
func (m *Match) EndRound() error {
	for i, score := range m.currentRound.Kills {
		kills := score[0]
		self := score[1]

		if kills == 3 {
			m.Players[i].AddSweep()
		}

		if self == -1 || kills == 3 || m.currentRound.Shots[i] {
			m.Players[i].AddShot()

			// This updates both the shot count and the sweep count (because
			// kills == 3 catches the sweep as well)
			err := globalDB.UpdatePlayer(m, &m.Players[i])
			if err != nil {
				return err
			}
		}
	}

	m.currentRound.Committed = time.Now()

	// Save the commit to the database
	commit := m.currentRound.asCommit()
	err := globalDB.AddCommit(m, &commit)
	if err != nil {
		return err
	}

	m.Rounds = append(m.Rounds, m.currentRound)
	// m.KillOrder = m.MakeKillOrder()

	// Reset the Round object
	m.currentRound = Round{
		Kills: [][]int{{0, 0}, {0, 0}, {0, 0}, {0, 0}},
		Shots: []bool{false, false, false, false},
	}

	return nil
}

// StartRound sets the initial state of player arrows.
func (m *Match) StartRound(sr StartRoundMessage) error {
	for i, as := range sr.Arrows {
		m.Players[i].State.Arrows = as
		m.Players[i].State.Alive = true
		m.Players[i].State.Hat = true
		m.Players[i].State.Lava = false
		m.Players[i].State.Killer = -2
	}
	m.currentRound.started = true
	return m.Tournament.Persist()
}

// ArrowUpdate updates the arrow state for a player
func (m *Match) ArrowUpdate(am ArrowMessage) error {
	m.Players[am.Player].State.Arrows = am.Arrows
	return m.sendPlayerUpdate(am.Player)
}

// ShieldUpdate updates the shield state for a player
func (m *Match) ShieldUpdate(sm ShieldMessage) error {
	m.Players[sm.Player].State.Shield = sm.State
	if !m.currentRound.started {
		// log.Print("Skipping update of non-started round")
		return nil
	}

	return m.sendPlayerUpdate(sm.Player)
}

// WingsUpdate updates the wings state for a player
func (m *Match) WingsUpdate(wm WingsMessage) error {
	m.Players[wm.Player].State.Wings = wm.State
	if !m.currentRound.started {
		// log.Print("Skipping update of non-started round")
		return nil
	}

	return m.sendPlayerUpdate(wm.Player)
}

// LavaOrb sets or unsets the lava for a player
func (m *Match) LavaOrb(lm LavaOrbMessage) error {
	m.Players[lm.Player].State.Lava = lm.State

	return m.sendPlayerUpdate(lm.Player)
}

// Kill records a Kill
func (m *Match) Kill(km KillMessage) error {
	m.Players[km.Player].State.Alive = false
	m.Players[km.Player].State.Killer = km.Killer

	if km.Killer == EnvironmentKill {
		m.Players[km.Player].AddSelf()
		m.currentRound.AddSelf(km.Player)

		return globalDB.UpdatePlayer(m, &m.Players[km.Player])
	} else if km.Killer == km.Player {
		m.Players[km.Player].AddSelf()
		m.currentRound.AddSelf(km.Player)

		return globalDB.UpdatePlayer(m, &m.Players[km.Killer])
	}

	m.Players[km.Killer].AddKills(1)
	m.currentRound.AddKill(km.Killer)
	return globalDB.UpdatePlayer(m, &m.Players[km.Killer])
}

// Start starts the match
func (m *Match) Start(c *gin.Context) error {
	if !m.Started.IsZero() {
		return errors.New("match already started")
	}

	log.Printf("Starting match %d", m.Index)

	for i := range m.Players {
		// m.Players[i].Reset()
		m.Players[i].Match = m
	}

	// Set the casters
	m.Casters = m.Tournament.Casters

	// Increment the current match, but only if we're not at the first.
	// if m.Index != 0 {
	// 	log.Printf("Increasing current from %d", m.Tournament.Current)
	// 	m.Tournament.Current++
	// } else {
	// 	log.Print("Not increasing current when starting first match")
	// }

	m.Started = time.Now()
	return globalDB.SaveMatch(m)
}

// End signals that the match has ended
//
// It is also the place that moves players into either the Runnerup bracket
// or into their place in the semis.
func (m *Match) End(c *gin.Context) error {
	log.Printf("Ending match %d", m.Index)
	if !m.Ended.IsZero() {
		return errors.New("match already ended")
	}

	// Give points based on performance
	scores := []int{
		scoreWinner,
		scoreSecond,
		scoreThird,
		scoreFourth,
	}

	// Give more points if we're in the finals
	multiplier := 1.0
	if m.Kind == final {
		multiplier = FinalMultiplier(len(m.Tournament.Matches))
		log.Printf("Setting final multiplier to be %.2f", multiplier)
	}

	for x, k := range m.MakeKillOrder() {
		// Give the winner a shot
		if x == 0 {
			m.Players[k].AddShot()
		}

		m.Players[k].MatchScore = int(float64(scores[x]) * multiplier)

		err := globalDB.UpdatePlayer(m, &m.Players[k])
		if err != nil {
			return err
		}
	}

	m.Ended = time.Now()
	err := globalDB.SaveMatch(m)
	if err != nil {
		return err
	}

	if m.Kind == final {
		if err := m.Tournament.End(); err != nil {
			return err
		}
	} else {
		if err := m.Tournament.MovePlayers(m); err != nil {
			return err
		}
	}

	err = m.Tournament.PublishNext()
	if err != nil && err != ErrPublishDisconnected {
		m.Tournament.server.log.Info("Publishing next match failed", zap.Error(err))
	}

	return nil
}

// Reset resets all the player scores to zero and removes all the commits
func (m *Match) Reset() error {
	// Reset dem players
	for i := range m.Players {
		m.Players[i].Reset()
	}

	// And remove all the rounds
	m.Rounds = make([]Round, 0)

	// And reset the start time
	m.Started = time.Time{}

	return globalDB.SaveMatch(m)
}

// Autoplay runs through the entire match simulating real play
func (m *Match) Autoplay() error {
	if !m.IsStarted() {
		err := m.Start(nil)
		if err != nil {
			log.Printf("Failed to start match: %+v", err)
			return err
		}
	}
	for !m.CanEnd() {
		m.Commit(NewAutoplayRound())
	}
	return m.End(nil)
}

// SetTime sets the scheduled time based on the Pause attribute
func (m *Match) SetTime(c *gin.Context, minutes int) error {
	m.Scheduled = time.Now().Add(time.Minute * time.Duration(minutes))
	return globalDB.SaveMatch(m)
}

// IsStarted returns boolean whether the match has started or not
func (m *Match) IsStarted() bool {
	return !m.Started.IsZero()
}

// IsEnded returns boolean whether the match has ended or not
func (m *Match) IsEnded() bool {
	return !m.Ended.IsZero()
}

// IsScheduled returns boolean whether the match has been scheduled or not
func (m *Match) IsScheduled() bool {
	return !m.Scheduled.IsZero()
}

// CanStart returns boolean the match can be controlled or not
func (m *Match) CanStart() bool {
	return !m.IsStarted() && !m.IsEnded()
}

// CanEnd returns boolean whether the match can be ended or not
func (m *Match) CanEnd() bool {
	if !m.IsOpen() {
		return false
	}
	for _, p := range m.Players {
		if p.Kills >= m.Length {
			return true
		}
	}
	return false
}

// IsOpen returns boolean the match can be controlled or not
func (m *Match) IsOpen() bool {
	return m.IsStarted() && !m.IsEnded()
}

// MakeKillOrder returns the score in order of the number of kills in the match.
func (m *Match) MakeKillOrder() (ret []int) {
	ps := SortByKills(m.Players)
	for _, p := range ps {
		for i, o := range m.Players {
			if p.Name() == o.Name() {
				ret = append(ret, i)
				break
			}
		}
	}

	return
}

// ArchersHarmed returns the number of killed archers during the match
func (m *Match) ArchersHarmed() int {
	ret := 0

	for _, r := range m.Rounds {
		for _, k := range r.Kills {
			ret += k[0]

			// If someone suicided, it shows up as a minus one. This means
			// an archer was harmed and should count towards the total.
			if k[1] == -1 {
				ret++
			}
		}
	}

	return ret
}

// Duration returns how long the match took
func (m *Match) Duration() time.Duration {
	return m.Ended.Sub(m.Started)
}

// NewMatchCommit makes a new MatchCommit object from a CommitRequest
func NewMatchCommit(c CommitRequest) Round {
	states := c.State
	m := Round{
		[][]int{
			{states[0].Ups, states[0].Downs},
			{states[1].Ups, states[1].Downs},
			{states[2].Ups, states[2].Downs},
			{states[3].Ups, states[3].Downs},
		},
		[]bool{
			states[0].Shot,
			states[1].Shot,
			states[2].Shot,
			states[3].Shot,
		},
		time.Now(),
		false,
	}

	return m
}

// IsShotUpdate returns true if the only thing that happened was shots
func (r *Round) IsShotUpdate() bool {
	for _, y := range r.Kills {
		for _, z := range y {
			if z != 0 {
				return false
			}
		}
	}

	for _, s := range r.Shots {
		if s {
			return true
		}
	}

	return false
}

// NewAutoplayRound fakes player activity in a round
//
// It randomizes how many kills the players get, and it randomizes
// shots every now and again. This does not fully represent actual
// gameplay, since technically all four players could get a sweep in
// the same match. However, since this is for testing purposes it is
// acceptable that such is the case.
func NewAutoplayRound() Round {
	r := Round{
		[][]int{{0, 0}, {0, 0}, {0, 0}, {0, 0}},
		[]bool{false, false, false, false},
		time.Now(),
		false,
	}

	rand.Seed(time.Now().UnixNano())
	for x := 0; x < 4; x++ {
		y := rand.Intn(100)
		// 5% of the times - sweep
		// 20% of the times - 2 kills
		// 70% of the times - 1 kill
		if y <= 5 {
			r.Kills[x][0] = 3
		} else if y <= 20 {
			r.Kills[x][0] = 2
		} else if y <= 70 {
			r.Kills[x][0] = 1
		}

		// 10% of the time - accidental self
		if rand.Intn(10)%10 == 0 {
			r.Kills[x][1] = -1
		}
	}

	return r
}

// AddKill adds one kill to the specified player
func (r *Round) AddKill(p int) {
	if len(r.Kills) == 0 {
		r.Reset()
	}

	r.Kills[p][0] += 1
}

// AddSelf adds one self to the specified player
func (r *Round) AddSelf(p int) {
	if len(r.Kills) == 0 {
		r.Reset()
	}

	r.Kills[p][1] -= 1
}

func (r *Round) Reset() {
	r.Kills = [][]int{{0, 0}, {0, 0}, {0, 0}, {0, 0}}
	r.Shots = []bool{false, false, false, false}
}

func (r *Round) asCommit() Commit {
	return Commit{
		P1up:   r.Kills[0][0],
		P1down: r.Kills[0][1],
		P1shot: r.Shots[0],

		P2up:   r.Kills[1][0],
		P2down: r.Kills[1][1],
		P2shot: r.Shots[1],

		P3up:   r.Kills[2][0],
		P3down: r.Kills[2][1],
		P3shot: r.Shots[2],

		P4up:   r.Kills[3][0],
		P4down: r.Kills[3][1],
		P4shot: r.Shots[3],

		Committed: r.Committed,
		started:   r.started,
	}
}
