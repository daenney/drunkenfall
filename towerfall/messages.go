package towerfall

import (
	"time"
)

type PlayerStateUpdateMessage struct {
	Tournament uint         `json:"tournament"`
	Match      int          `json:"match"`
	State      *PlayerState `json:"state"`
}

type MatchUpdateMessage struct {
	Tournament uint           `json:"tournament"`
	Match      *Match         `json:"match"`
	Players    []*PlayerState `json:"player_states"`
}

// StartPlayMessage is sent to the game whenever one of the shot girls
// have indicated that the next round can start
type StartPlayMessage struct{}

// TournamentCompleteMessage is sent to the game whenever one of the shot girls
// have indicated that the next round can start
type TournamentCompleteMessage struct{}

// Constant strings for use as kinds when communicating with the game
const (
	gMatch      = "match"
	gConnect    = "game_connected"
	gDisconnect = "game_disconnected"
	gStartPlay  = "start_play"
	gComplete   = "tournament_complete"
)

// GameMatchMessage is the message sent to the game about the
// configuration of the next match
type GameMatchMessage struct {
	Players    []GamePlayer `json:"players"`
	Tournament string       `json:"tournament"`
	Level      string       `json:"level"`
	Length     int          `json:"length"`
	Ruleset    string       `json:"ruleset"`
	Kind       string       `json:"kind"`
}

// GamePlayer is a player object to be consumed by the game
type GamePlayer struct {
	TopName    string `json:"top_name"`
	BottomName string `json:"bottom_name"`
	Color      int    `json:"color"`
	ArcherType int    `json:"archer_type"`
}

type Message struct {
	ID        uint        `json:"id"`
	MatchID   uint        `json:"match_id"`
	Type      string      `json:"type"`
	Data      interface{} `json:"data" sql:"-"`
	JSON      string      `json:"-"`
	Timestamp time.Time   `json:"timestamp"`
}

const EnvironmentKill = -1

// Kill reasons
// nolint
const (
	rArrow = iota
	rExplosion
	rBrambles
	rJumpedOn
	rLava
	rShock
	rSpikeBall
	rFallingObject
	rSquish
	rCurse
	rMiasma
	rEnemy
	rChalice
)

// Arrows
// nolint
const (
	aNormal = iota
	aBomb
	aSuperBomb
	aLaser
	aBramble
	aDrill
	aBolt
	aToy
	aFeather
	aTrigger
	aPrism
)

// Message types
// nolint
const (
	inKill       = "kill"
	inRoundStart = "round_start"
	inRoundEnd   = "round_end"
	inMatchStart = "match_start"
	inMatchEnd   = "match_end"
	inPickup     = "arrows_collected"
	inShot       = "arrow_shot"
	inShield     = "shield_state"
	inWings      = "wings_state"
	inOrbLava    = "lava_orb_state"
	// TODO(thiderman): Non-player orbs are not implemented
	inOrbSlow   = "slow_orb_state"
	inOrbDark   = "dark_orb_state"
	inOrbScroll = "scroll_orb_state"
)

type KillMessage struct {
	Player int `json:"player"`
	Killer int `json:"killer"`
	Cause  int `json:"cause"`
}

type ArrowMessage struct {
	Player int    `json:"player"`
	Arrows Arrows `json:"arrows"`
}

type ShieldMessage struct {
	Player int  `json:"player"`
	State  bool `json:"state"`
}

type WingsMessage struct {
	Player int  `json:"player"`
	State  bool `json:"state"`
}

type SlowOrbMessage struct {
	State bool `json:"state"`
}

type DarkOrbMessage struct {
	State bool `json:"state"`
}

type ScrollOrbMessage struct {
	State bool `json:"state"`
}

type LavaOrbMessage struct {
	Player int  `json:"player"`
	State  bool `json:"state"`
}

// List of integers where one item is an arrow type as described in
// the arrow types above.
type Arrows []int

type StartRoundMessage struct {
	Arrows []Arrows `json:"arrows"`
}
