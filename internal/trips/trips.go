package trips

import (
	"errors"
)

type Status string

const (
	Requested  Status = "requested"
	Accepted   Status = "accepted"
	InProgress Status = "in_progress"
	Completed  Status = "completed"
	Cancelled  Status = "cancelled"
)

var Statuses = []string{"requested", "accepted", "in_progress", "completed", "cancelled"}

type Action string

const (
	Accept   Action = "accept"
	Start    Action = "start"
	Complete Action = "complete"
	Cancel   Action = "cancel"
)

var ErrInvalidTransition = errors.New("invalid transition")

// Dictates what happens to a trip
// Anything not listed here is refused
var rules = map[Action]map[Status]Status{
	Accept:   {Requested: Accepted},
	Start:    {Accepted: InProgress},
	Complete: {InProgress: Completed},
	Cancel:   {Requested: Cancelled, Accepted: Cancelled},
}

// Next returns the status trip moves to, or ErrInvalidTransition

func Next(from Status, a Action) (Status, error) {
	to, ok := rules[a][from]
	if !ok {
		return "", ErrInvalidTransition
	}
	return to, nil
}

// Dictates what an action does to the assigned driver
// ok is false when the action leaves the driver alone
func DriverStatus(a Action) (Status string, ok bool) {
	switch a {
	case Accept:
		return "on_trip", true
	case Complete, Cancel:
		return "available", true
	}
	return "", false
}
