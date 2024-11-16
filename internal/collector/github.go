// NOTE: The github events polling API no longer supports "real-time" event streaming.
// TODO: Implement etag walking
// TODO: Implement HA event de-duplication
package collector

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"

	cloudevents "github.com/cloudevents/sdk-go/v2"
	"github.com/fraser-isbester/fwd/pkg/types"
	"github.com/google/go-github/v53/github"
	"golang.org/x/time/rate"
)

// GitHubCollector implementation
type GitHubCollector struct {
	client   *github.Client
	limiter  *rate.Limiter
	orgName  string
	tracker  *EventTracker
	stopChan chan struct{}
	wg       sync.WaitGroup
}

func NewGitHubCollector(client *github.Client, orgName string) *GitHubCollector {
	return &GitHubCollector{
		client:   client,
		orgName:  orgName,
		limiter:  rate.NewLimiter(rate.Every(time.Second/300), 3),
		tracker:  NewEventTracker(),
		stopChan: make(chan struct{}),
	}
}

func (gc *GitHubCollector) Name() string {
	return "github"
}

func (gc *GitHubCollector) convertGitHubEvent(ghEvent *github.Event) (*types.Event, error) {
	payload, err := ghEvent.ParsePayload()
	if err != nil {
		return nil, fmt.Errorf("failed to parse payload: %v", err)
	}

	data, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal payload: %v", err)
	}

	eventType := fmt.Sprintf("github.%s", ghEvent.GetType())
	source := fmt.Sprintf("//github.com/%s/%s", gc.orgName, ghEvent.Repo.GetName())

	event := &types.Event{
		ID:          ghEvent.GetID(),
		Source:      source,
		SpecVersion: cloudevents.VersionV1,
		Type:        eventType,
		Time:        ghEvent.GetCreatedAt().UTC(),
		Data:        data,
		Subject:     ghEvent.Actor.GetLogin(),
		Extensions: map[string]any{
			"org":      gc.orgName,
			"repo":     ghEvent.Repo.GetName(),
			"actor":    ghEvent.Actor.GetLogin(),
			"raw_type": ghEvent.GetType(),
		},
	}

	return event, nil
}

func (gc *GitHubCollector) collectEvents(ctx context.Context, eventChan chan<- *types.Event) error {
	cutoff := time.Now().Add(-24 * time.Hour)

	repoOpts := &github.RepositoryListByOrgOptions{
		ListOptions: github.ListOptions{PerPage: 100},
		Type:        "all",
	}

	for {
		if err := gc.limiter.Wait(ctx); err != nil {
			return err
		}

		repos, resp, err := gc.client.Repositories.ListByOrg(ctx, gc.orgName, repoOpts)
		if err != nil {
			return fmt.Errorf("error listing repos: %v", err)
		}

		for _, repo := range repos {
			if err := gc.limiter.Wait(ctx); err != nil {
				return err
			}

			repoName := repo.GetName()
			if repoName != "elixir" {
				continue
			}
			lastEventID := gc.tracker.GetLastEventID(repoName)

			// Event pagination loop
			eventOpts := &github.ListOptions{PerPage: 100}
			var allEvents []*github.Event
			var latestEventID string

			for {
				if err := gc.limiter.Wait(ctx); err != nil {
					return err
				}

				events, eventResp, err := gc.client.Activity.ListRepositoryEvents(
					ctx,
					gc.orgName,
					repoName,
					eventOpts,
				)
				if err != nil {
					log.Printf("Error fetching events for %s: %v", repoName, err)
					break
				}

				if len(events) == 0 {
					break
				}

				// Store the latest event ID from the first page
				if latestEventID == "" && len(events) > 0 {
					latestEventID = events[0].GetID()
				}

				allEvents = append(allEvents, events...)

				if eventResp.NextPage == 0 {
					break
				}
				eventOpts.Page = eventResp.NextPage
			}

			if len(allEvents) == 0 {
				continue
			}

			log.Printf("Processing %d events for %s (latest: %s, last seen: %s)",
				len(allEvents), repoName, latestEventID, lastEventID)

			// Events are returned newest first
			foundLastSeen := false
			for _, ghEvent := range allEvents {
				currentID := ghEvent.GetID()

				// If we've seen this event before, mark it and skip
				if currentID == lastEventID {
					foundLastSeen = true
					log.Printf("Found last seen event %s for %s", lastEventID, repoName)
					continue
				}

				// If we haven't found the last seen event yet, these events are newer
				// so we should process them
				if !foundLastSeen {
					eventTime := ghEvent.GetCreatedAt()
					if eventTime.Before(cutoff) {
						log.Printf("Event %s for %s (%s) is older than cutoff, stopping",
							currentID, repoName, eventTime.String())
						break
					}

					log.Printf("Processing new event %s for %s (%s)",
						currentID, repoName, eventTime.String())

					event, err := gc.convertGitHubEvent(ghEvent)
					if err != nil {
						log.Printf("Error converting event %s: %v", currentID, err)
						continue
					}

					select {
					case eventChan <- event:
						// Event sent successfully
					case <-ctx.Done():
						return ctx.Err()
					}
				}
			}

			// Always update to the newest event ID
			if latestEventID != "" {
				gc.tracker.UpdateLastEventID(repoName, latestEventID)
			}
		}

		if resp.NextPage == 0 {
			break
		}
		repoOpts.Page = resp.NextPage
	}

	return nil
}

func (gc *GitHubCollector) Start(ctx context.Context, eventChan chan<- *types.Event) error {
	log.Printf("Starting GitHub collector for org: %s", gc.orgName)

	gc.wg.Add(1)
	go func() {
		defer gc.wg.Done()
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()

		if err := gc.collectEvents(ctx, eventChan); err != nil {
			log.Printf("Error in initial collection: %v", err)
		}

		for {
			select {
			case <-ticker.C:
				if err := gc.collectEvents(ctx, eventChan); err != nil {
					log.Printf("Error collecting events: %v", err)
				}
			case <-gc.stopChan:
				return
			case <-ctx.Done():
				return
			}
		}
	}()

	return nil
}

func (gc *GitHubCollector) Stop() error {
	close(gc.stopChan)
	gc.wg.Wait()
	return nil
}
