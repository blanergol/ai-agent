package tools

import (
	"fmt"
	"strings"
	"sync"
	"time"
)

const (
	IncidentStatusInvestigating = "investigating"
	IncidentStatusMitigating    = "mitigating"
	IncidentStatusMonitoring    = "monitoring"
	IncidentStatusResolved      = "resolved"
)

// ServiceProfile describes deterministic metadata for one production service.
type ServiceProfile struct {
	Service         string
	Tier            string
	OwnerTeam       string
	PagerDuty       string
	RunbookID       string
	BusinessImpact  string
	Dependencies    []string
	DefaultSeverity string
}

// OnCallShift describes deterministic on-call contacts for one team.
type OnCallShift struct {
	Team       string
	Primary    string
	Secondary  string
	Timezone   string
	HandoffUTC string
}

// Runbook describes deterministic operational guidance.
type Runbook struct {
	ID              string
	Title           string
	DefaultScenario string
	ScenarioSteps   map[string][]string
	Verification    []string
}

// IncidentEvent is one timeline event attached to an incident.
type IncidentEvent struct {
	Timestamp time.Time
	Type      string
	Message   string
	Actor     string
}

// Incident is the domain object persisted in the private in-memory store.
type Incident struct {
	ID             string
	Service        string
	Severity       string
	Status         string
	Summary        string
	Source         string
	AssigneeTeam   string
	CustomerImpact bool
	RunbookID      string
	CreatedAt      time.Time
	UpdatedAt      time.Time
	Timeline       []IncidentEvent
}

// CreateIncidentInput is validated by .incident.create and applied by IncidentStore.
type CreateIncidentInput struct {
	Service        string
	Summary        string
	Severity       string
	Source         string
	AssigneeTeam   string
	CustomerImpact bool
}

// UpdateIncidentInput is validated by .incident.update and applied by IncidentStore.
type UpdateIncidentInput struct {
	IncidentID   string
	Status       string
	Note         string
	NextAction   string
	AssigneeTeam string
}

// IncidentStore is deterministic private in-memory storage for incident tools.
type IncidentStore struct {
	mu        sync.RWMutex
	seq       int
	incidents map[string]Incident
	services  map[string]ServiceProfile
	onCall    map[string]OnCallShift
	runbooks  map[string]Runbook
}

// NewIncidentStore initializes deterministic in-memory datasets used by tools.
func NewIncidentStore() *IncidentStore {
	return &IncidentStore{
		incidents: map[string]Incident{},
		services: map[string]ServiceProfile{
			"payments-api": {
				Service:         "payments-api",
				Tier:            "tier-1",
				OwnerTeam:       "payments-platform",
				PagerDuty:       "pd-payments-primary",
				RunbookID:       "rb-payments-latency",
				BusinessImpact:  "Card payments and checkout confirmations may fail.",
				Dependencies:    []string{"postgres-main", "redis-cache", "event-bus"},
				DefaultSeverity: "sev2",
			},
			"auth-gateway": {
				Service:         "auth-gateway",
				Tier:            "tier-0",
				OwnerTeam:       "identity-platform",
				PagerDuty:       "pd-identity-primary",
				RunbookID:       "rb-auth-errors",
				BusinessImpact:  "Users are unable to sign in and refresh sessions.",
				Dependencies:    []string{"session-store", "idp-adapter"},
				DefaultSeverity: "sev1",
			},
			"notifications-worker": {
				Service:         "notifications-worker",
				Tier:            "tier-2",
				OwnerTeam:       "engagement-platform",
				PagerDuty:       "pd-engagement-primary",
				RunbookID:       "rb-notifications-backlog",
				BusinessImpact:  "Delayed email/push delivery for non-critical workflows.",
				Dependencies:    []string{"message-queue", "smtp-relay"},
				DefaultSeverity: "sev3",
			},
			"analytics-ingest": {
				Service:         "analytics-ingest",
				Tier:            "tier-3",
				OwnerTeam:       "data-platform",
				PagerDuty:       "pd-data-primary",
				RunbookID:       "rb-analytics-lag",
				BusinessImpact:  "Reporting is stale, operational systems remain available.",
				Dependencies:    []string{"kafka-cluster", "warehouse-loader"},
				DefaultSeverity: "sev4",
			},
		},
		onCall: map[string]OnCallShift{
			"payments-platform": {
				Team:       "payments-platform",
				Primary:    "alina.sokolova",
				Secondary:  "mark.chan",
				Timezone:   "Europe/Moscow",
				HandoffUTC: "06:00",
			},
			"identity-platform": {
				Team:       "identity-platform",
				Primary:    "irina.kim",
				Secondary:  "josh.baker",
				Timezone:   "UTC",
				HandoffUTC: "00:00",
			},
			"engagement-platform": {
				Team:       "engagement-platform",
				Primary:    "yuri.petrov",
				Secondary:  "eva.lee",
				Timezone:   "Europe/Berlin",
				HandoffUTC: "07:00",
			},
			"data-platform": {
				Team:       "data-platform",
				Primary:    "nikolay.ivanov",
				Secondary:  "mia.smith",
				Timezone:   "UTC",
				HandoffUTC: "08:00",
			},
			"noc": {
				Team:       "noc",
				Primary:    "noc-primary",
				Secondary:  "noc-secondary",
				Timezone:   "UTC",
				HandoffUTC: "00:00",
			},
		},
		runbooks: map[string]Runbook{
			"rb-payments-latency": {
				ID:              "rb-payments-latency",
				Title:           "Payments API Latency",
				DefaultScenario: "high-latency",
				ScenarioSteps: map[string][]string{
					"high-latency": {
						"Check p95/p99 latency and error-rate dashboards for payments-api.",
						"Validate postgres connection pool saturation and queue depth.",
						"Enable degraded mode to skip optional risk checks if threshold exceeded.",
					},
					"timeout-spike": {
						"Inspect upstream dependency timeouts and retry storms.",
						"Scale API pods and confirm circuit breaker state.",
					},
				},
				Verification: []string{
					"p95 latency below 400ms for 15 minutes",
					"5xx error rate below 1%",
				},
			},
			"rb-auth-errors": {
				ID:              "rb-auth-errors",
				Title:           "Authentication Error Spike",
				DefaultScenario: "login-failure",
				ScenarioSteps: map[string][]string{
					"login-failure": {
						"Validate token issuer reachability and certificate expiry.",
						"Check idp-adapter health and recent deploy changes.",
						"Temporarily extend session TTL if refresh endpoint degraded.",
					},
				},
				Verification: []string{
					"Successful login ratio above 98%",
					"Token refresh success rate above 99%",
				},
			},
			"rb-notifications-backlog": {
				ID:              "rb-notifications-backlog",
				Title:           "Notifications Queue Backlog",
				DefaultScenario: "queue-backlog",
				ScenarioSteps: map[string][]string{
					"queue-backlog": {
						"Check queue consumer lag and worker throughput.",
						"Pause low-priority campaigns to free worker capacity.",
					},
				},
				Verification: []string{
					"Queue lag under 500 messages",
				},
			},
			"rb-analytics-lag": {
				ID:              "rb-analytics-lag",
				Title:           "Analytics Pipeline Lag",
				DefaultScenario: "ingest-delay",
				ScenarioSteps: map[string][]string{
					"ingest-delay": {
						"Check kafka consumer group lag and partition skew.",
						"Validate warehouse loader retries and dead-letter queue size.",
					},
				},
				Verification: []string{
					"Data freshness under 30 minutes",
				},
			},
			"rb-generic-investigation": {
				ID:              "rb-generic-investigation",
				Title:           "Generic Service Investigation",
				DefaultScenario: "generic",
				ScenarioSteps: map[string][]string{
					"generic": {
						"Confirm customer-facing impact and scope.",
						"Collect recent deploys and dependency health signals.",
						"Escalate to NOC if owner team is unknown.",
					},
				},
				Verification: []string{
					"Customer impact statement confirmed",
				},
			},
		},
	}
}

// LookupService returns deterministic service metadata.
func (s *IncidentStore) LookupService(service string) (ServiceProfile, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	key := normalizeKey(service)
	profile, ok := s.services[key]
	if !ok {
		return ServiceProfile{}, fmt.Errorf("service not found: %s", strings.TrimSpace(service))
	}
	return profile, nil
}

// LookupOnCall returns deterministic on-call contacts for a team.
func (s *IncidentStore) LookupOnCall(team string) (OnCallShift, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	key := normalizeKey(team)
	shift, ok := s.onCall[key]
	if !ok {
		return OnCallShift{}, fmt.Errorf("on-call not found for team: %s", strings.TrimSpace(team))
	}
	return shift, nil
}

// LookupRunbook resolves runbook details and scenario-specific steps.
func (s *IncidentStore) LookupRunbook(runbookID, scenario string) (Runbook, string, []string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	key := normalizeKey(runbookID)
	runbook, ok := s.runbooks[key]
	if !ok {
		return Runbook{}, "", nil, fmt.Errorf("runbook not found: %s", strings.TrimSpace(runbookID))
	}

	selectedScenario := normalizeKey(scenario)
	if selectedScenario == "" {
		selectedScenario = runbook.DefaultScenario
	}
	steps, ok := runbook.ScenarioSteps[selectedScenario]
	if !ok {
		steps = runbook.ScenarioSteps[runbook.DefaultScenario]
		selectedScenario = runbook.DefaultScenario
	}
	out := make([]string, len(steps))
	copy(out, steps)
	return runbook, selectedScenario, out, nil
}

// CreateIncident creates a new incident and writes an initial timeline event.
func (s *IncidentStore) CreateIncident(input CreateIncidentInput, now time.Time) (Incident, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	serviceKey := normalizeKey(input.Service)
	profile, ok := s.services[serviceKey]
	if !ok {
		return Incident{}, fmt.Errorf("service not found: %s", strings.TrimSpace(input.Service))
	}

	s.seq++
	id := fmt.Sprintf("INC-%06d", s.seq)
	severity := normalizeSeverity(input.Severity)
	if severity == "" {
		severity = normalizeSeverity(profile.DefaultSeverity)
	}
	assignee := strings.TrimSpace(input.AssigneeTeam)
	if assignee == "" {
		assignee = profile.OwnerTeam
	}
	source := strings.TrimSpace(input.Source)
	if source == "" {
		source = "internal.bundle"
	}
	timestamp := now.UTC()
	incident := Incident{
		ID:             id,
		Service:        profile.Service,
		Severity:       severity,
		Status:         IncidentStatusInvestigating,
		Summary:        strings.TrimSpace(input.Summary),
		Source:         source,
		AssigneeTeam:   assignee,
		CustomerImpact: input.CustomerImpact,
		RunbookID:      profile.RunbookID,
		CreatedAt:      timestamp,
		UpdatedAt:      timestamp,
		Timeline: []IncidentEvent{
			{
				Timestamp: timestamp,
				Type:      "created",
				Message:   "Incident created by automation pipeline.",
				Actor:     "agent-core",
			},
		},
	}
	s.incidents[id] = incident
	return incident, nil
}

// UpdateIncident applies mutable changes to an existing incident.
func (s *IncidentStore) UpdateIncident(input UpdateIncidentInput, now time.Time) (Incident, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	incidentID := strings.TrimSpace(input.IncidentID)
	incident, ok := s.incidents[incidentID]
	if !ok {
		return Incident{}, fmt.Errorf("incident not found: %s", incidentID)
	}

	changed := false
	timestamp := now.UTC()

	status := normalizeStatus(input.Status)
	if status != "" && status != incident.Status {
		incident.Status = status
		incident.Timeline = append(incident.Timeline, IncidentEvent{
			Timestamp: timestamp,
			Type:      "status",
			Message:   "Status changed to " + status,
			Actor:     "agent-core",
		})
		changed = true
	}

	assignee := strings.TrimSpace(input.AssigneeTeam)
	if assignee != "" && assignee != incident.AssigneeTeam {
		incident.AssigneeTeam = assignee
		incident.Timeline = append(incident.Timeline, IncidentEvent{
			Timestamp: timestamp,
			Type:      "assignee",
			Message:   "Assigned to team " + assignee,
			Actor:     "agent-core",
		})
		changed = true
	}

	note := strings.TrimSpace(input.Note)
	if note != "" {
		incident.Timeline = append(incident.Timeline, IncidentEvent{
			Timestamp: timestamp,
			Type:      "note",
			Message:   note,
			Actor:     "agent-core",
		})
		changed = true
	}

	nextAction := strings.TrimSpace(input.NextAction)
	if nextAction != "" {
		incident.Timeline = append(incident.Timeline, IncidentEvent{
			Timestamp: timestamp,
			Type:      "next_action",
			Message:   nextAction,
			Actor:     "agent-core",
		})
		changed = true
	}

	if !changed {
		return Incident{}, fmt.Errorf("incident update requires at least one change")
	}

	incident.UpdatedAt = timestamp
	s.incidents[incidentID] = incident
	return incident, nil
}

// GetIncident returns one incident by ID.
func (s *IncidentStore) GetIncident(incidentID string) (Incident, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	incident, ok := s.incidents[strings.TrimSpace(incidentID)]
	return incident, ok
}

func normalizeKey(v string) string {
	return strings.ToLower(strings.TrimSpace(v))
}

func normalizeSeverity(v string) string {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "sev1", "critical", "p1":
		return "sev1"
	case "sev2", "high", "p2":
		return "sev2"
	case "sev3", "medium", "p3":
		return "sev3"
	case "sev4", "low", "p4":
		return "sev4"
	default:
		return ""
	}
}

func normalizeStatus(v string) string {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case IncidentStatusInvestigating:
		return IncidentStatusInvestigating
	case IncidentStatusMitigating:
		return IncidentStatusMitigating
	case IncidentStatusMonitoring:
		return IncidentStatusMonitoring
	case IncidentStatusResolved:
		return IncidentStatusResolved
	default:
		return ""
	}
}
