//go:build !linux

package main

import (
	"context"
	"log"

	"github.com/vishu42/openplan/internal/activities"
	"github.com/vishu42/openplan/internal/config"
	"github.com/vishu42/openplan/internal/runhelper"
	"github.com/vishu42/openplan/internal/runuser"
)

// newIsolation runs every branch as the developer: off Linux there are no pool
// users. The executor runs tenant Terraform only in its Linux image.
func newIsolation() (activities.Isolation, error) {
	helper, err := runhelper.NewClient()
	if err != nil {
		return activities.Isolation{}, err
	}
	log.Print("executor: not on Linux, so every session runs as the current user, without isolation (development only)")
	return activities.Isolation{Users: runuser.DevelopmentUsers(runuser.PoolSize), Helper: developmentHelper{helper}}, nil
}

func checkIsolation(cfg config.ExecutorConfig, users []runuser.User) error {
	return checkSessionLimit(cfg.MaxSessions, users)
}

// developmentHelper is the helper without its kills. Every session runs as the
// developer here, so killing all of the user's processes would end their whole
// login, and clearing the user's scratch files would delete theirs. The real
// helper refuses both outside the pool, which would fail every command.
type developmentHelper struct {
	runhelper.Client
}

func (developmentHelper) Kill(context.Context, runuser.User) error {
	return nil
}

// Reclaim only empties the workspace, in process: the executor and the branch
// are the same user.
func (developmentHelper) Reclaim(_ context.Context, _ runuser.User, workspace string) error {
	if workspace == "" {
		return nil
	}
	return runhelper.EmptyDir(workspace)
}
