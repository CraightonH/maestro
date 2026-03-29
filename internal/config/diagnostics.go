package config

import (
	"fmt"
	"slices"
	"sort"
	"strings"
)

const (
	filterRelationDisjoint = iota
	filterRelationEquivalent
	filterRelationASubsumesB
	filterRelationBSubsumesA
	filterRelationOverlap
)

type issueFilterSpec struct {
	Labels          []string
	States          []string
	Assignee        string
	IIDsIgnored     bool
	GitLabEpicBroad bool
}

type epicFilterSpec struct {
	Labels []string
	States []string
	IIDs   []int
}

type routeSource struct {
	Source SourceConfig
	Prefix string
	Intake issueFilterSpec
	Epic   epicFilterSpec
}

type filterRelation struct {
	Kind int
}

// DiagnoseConfig returns warnings for configs that are valid but may cause
// unexpected behavior, such as lifecycle transitions that could re-dispatch
// the same issue to the same source in an infinite loop or sources whose route
// filters likely overlap.
func DiagnoseConfig(cfg *Config) []string {
	var warnings []string
	for _, source := range cfg.Sources {
		prefix := source.LabelPrefix
		if prefix == "" {
			prefix = cfg.Defaults.LabelPrefix
		}
		if prefix == "" {
			prefix = "maestro"
		}

		onComplete := ResolveLifecycleTransition(cfg.Defaults.OnComplete, source.OnComplete)
		if transitionMayLoop(onComplete, source, prefix) {
			warnings = append(warnings, fmt.Sprintf(
				"source %q on_complete adds no routing label and no state change — completed issues may be re-dispatched by the same source",
				source.Name,
			))
		}

		onFailure := ResolveLifecycleTransition(cfg.Defaults.OnFailure, source.OnFailure)
		if transitionMayLoop(onFailure, source, prefix) {
			warnings = append(warnings, fmt.Sprintf(
				"source %q on_failure adds no routing label and no state change — failed issues may be re-dispatched by the same source",
				source.Name,
			))
		}
	}

	routes := make([]routeSource, 0, len(cfg.Sources))
	for _, source := range cfg.Sources {
		routes = append(routes, routeSource{
			Source: source,
			Prefix: normalizeDiagnosticPrefix(source.LabelPrefix),
			Intake: buildIssueFilterSpec(source),
			Epic:   buildEpicFilterSpec(source),
		})
	}

	for i := 0; i < len(routes); i++ {
		for j := i + 1; j < len(routes); j++ {
			if warning := diagnosePairCollision(routes[i], routes[j]); warning != "" {
				warnings = append(warnings, warning)
			}
		}
	}

	for _, route := range routes {
		warnings = append(warnings, diagnoseTransitionCollisions(cfg, route, routes)...)
	}

	sort.Strings(warnings)
	return dedupeStrings(warnings)
}

// transitionMayLoop returns true if a lifecycle transition would leave the
// issue eligible for re-dispatch by the same source. This happens when:
//   - the transition is explicitly configured (overrides defaults)
//   - it adds no labels (no routing label to move the issue elsewhere)
//   - it has no state change (issue stays in a filter-matching state)
//
// When the transition is nil (defaults apply), the framework automatically
// adds {prefix}:done or {prefix}:failed which blocks re-intake.
func transitionMayLoop(transition *LifecycleTransition, source SourceConfig, prefix string) bool {
	if transition == nil {
		return false // defaults add a blocking lifecycle label
	}
	if transition.State != "" {
		return false // state change likely moves issue out of filter
	}
	if len(transition.AddLabels) > 0 {
		return false // adds a routing label for another source
	}
	return true
}

func diagnosePairCollision(a routeSource, b routeSource) string {
	if !routesMayTouchSameTrackerItem(a, b) {
		return ""
	}

	intakeRelation := relateIssueFilters(a.Intake, b.Intake)
	if intakeRelation.Kind == filterRelationDisjoint {
		return ""
	}

	scope := pairScopeDescription(a, b)
	var detail string
	switch intakeRelation.Kind {
	case filterRelationEquivalent:
		detail = fmt.Sprintf("they use indistinguishable effective intake filters (%s)", describeIssueFilter(a.Intake))
	case filterRelationASubsumesB:
		detail = fmt.Sprintf("source %q subsumes %q: %s is broader than %s", a.Source.Name, b.Source.Name, describeIssueFilter(a.Intake), describeIssueFilter(b.Intake))
	case filterRelationBSubsumesA:
		detail = fmt.Sprintf("source %q subsumes %q: %s is broader than %s", b.Source.Name, a.Source.Name, describeIssueFilter(b.Intake), describeIssueFilter(a.Intake))
	default:
		detail = fmt.Sprintf("their effective intake filters still overlap (%s vs %s)", describeIssueFilter(a.Intake), describeIssueFilter(b.Intake))
	}

	var prefixNote string
	if a.Prefix != b.Prefix {
		prefixNote = fmt.Sprintf("They use different lifecycle prefixes (%q vs %q), so one source's reserved lifecycle labels do not block the other.", a.Prefix, b.Prefix)
	} else {
		prefixNote = fmt.Sprintf("They share lifecycle prefix %q; only the exact reserved labels %s:active/%s:retry/%s:done/%s:failed block intake.", a.Prefix, a.Prefix, a.Prefix, a.Prefix, a.Prefix)
	}

	notes := combinedRouteNotes(a, b)
	if notes != "" {
		notes = " " + notes
	}

	return fmt.Sprintf(
		"route collision: sources %q and %q may both dispatch the same %s because %s. %s%s",
		a.Source.Name,
		b.Source.Name,
		scope,
		detail,
		prefixNote,
		notes,
	)
}

func diagnoseTransitionCollisions(cfg *Config, route routeSource, routes []routeSource) []string {
	transitions := []struct {
		Name       string
		Transition *LifecycleTransition
	}{
		{
			Name:       "on_complete",
			Transition: ResolveLifecycleTransition(cfg.Defaults.OnComplete, route.Source.OnComplete),
		},
		{
			Name:       "on_failure",
			Transition: ResolveLifecycleTransition(cfg.Defaults.OnFailure, route.Source.OnFailure),
		},
	}

	var warnings []string
	for _, item := range transitions {
		if item.Transition == nil {
			continue
		}

		post := applyTransitionPreview(route.Intake, item.Transition)
		matches := make([]string, 0, len(routes))
		for _, other := range routes {
			if !routesMayTouchSameTrackerItem(route, other) {
				continue
			}
			if previewMayMatchFilter(post, other.Intake) {
				matches = append(matches, other.Source.Name)
			}
		}
		sort.Strings(matches)
		if len(matches) < 2 {
			continue
		}

		warnings = append(warnings, fmt.Sprintf(
			"route collision: source %q %s may leave the same tracker item eligible for multiple sources (%s). Resulting routing constraints are %s. Custom lifecycle transitions remove %s:active but do not add the default %s:done/%s:failed blocker, and routing labels remain visible to source filters.",
			route.Source.Name,
			item.Name,
			strings.Join(matches, ", "),
			describeIssueFilter(post),
			route.Prefix,
			route.Prefix,
			route.Prefix,
		))
	}
	return warnings
}

func buildIssueFilterSpec(source SourceConfig) issueFilterSpec {
	filter := source.EffectiveIssueFilter()
	spec := issueFilterSpec{
		Labels:      normalizeStringList(filter.Labels),
		States:      normalizeStringList(filter.States),
		Assignee:    strings.ToLower(strings.TrimSpace(filter.Assignee)),
		IIDsIgnored: len(filter.IIDs) > 0,
	}
	if source.Tracker == "gitlab-epic" && len(source.Filter.Labels) > 0 && len(spec.Labels) == 0 {
		spec.GitLabEpicBroad = true
	}
	return spec
}

func buildEpicFilterSpec(source SourceConfig) epicFilterSpec {
	filter := source.EffectiveEpicFilter()
	return epicFilterSpec{
		Labels: normalizeStringList(filter.Labels),
		States: normalizeStringList(filter.States),
		IIDs:   normalizeIntList(filter.IIDs),
	}
}

func describeIssueFilter(filter issueFilterSpec) string {
	parts := make([]string, 0, 3)
	if len(filter.Labels) > 0 {
		parts = append(parts, fmt.Sprintf("labels %s", formatStrings(filter.Labels)))
	}
	if len(filter.States) > 0 {
		parts = append(parts, fmt.Sprintf("states %s", formatStrings(filter.States)))
	}
	if filter.Assignee != "" {
		parts = append(parts, fmt.Sprintf("assignee %q", filter.Assignee))
	}
	if len(parts) == 0 {
		return "no label/state/assignee constraints"
	}
	return strings.Join(parts, ", ")
}

func relateIssueFilters(a issueFilterSpec, b issueFilterSpec) filterRelation {
	if !issueFiltersOverlap(a, b) {
		return filterRelation{Kind: filterRelationDisjoint}
	}
	aSubsumesB := issueFilterSubsumes(a, b)
	bSubsumesA := issueFilterSubsumes(b, a)
	switch {
	case aSubsumesB && bSubsumesA:
		return filterRelation{Kind: filterRelationEquivalent}
	case aSubsumesB:
		return filterRelation{Kind: filterRelationASubsumesB}
	case bSubsumesA:
		return filterRelation{Kind: filterRelationBSubsumesA}
	default:
		return filterRelation{Kind: filterRelationOverlap}
	}
}

func issueFiltersOverlap(a issueFilterSpec, b issueFilterSpec) bool {
	if a.Assignee != "" && b.Assignee != "" && a.Assignee != b.Assignee {
		return false
	}
	if len(a.States) > 0 && len(b.States) > 0 && !hasStringIntersection(a.States, b.States) {
		return false
	}
	return true
}

func issueFilterSubsumes(broader issueFilterSpec, narrower issueFilterSpec) bool {
	if broader.Assignee != "" && broader.Assignee != narrower.Assignee {
		return false
	}
	if !stateSetSubsumes(broader.States, narrower.States) {
		return false
	}
	return labelSetSubsumes(broader.Labels, narrower.Labels)
}

func routesMayTouchSameTrackerItem(a routeSource, b routeSource) bool {
	switch {
	case a.Source.Tracker == "linear" && b.Source.Tracker == "linear":
		return sameLinearProject(a.Source, b.Source)
	case a.Source.Tracker == "gitlab" && b.Source.Tracker == "gitlab":
		return sameGitLabProject(a.Source, b.Source)
	case a.Source.Tracker == "gitlab-epic" && b.Source.Tracker == "gitlab-epic":
		return sameGitLabGroup(a.Source, b.Source) && epicFiltersOverlap(a.Epic, b.Epic)
	case a.Source.Tracker == "gitlab" && b.Source.Tracker == "gitlab-epic":
		return gitLabProjectWithinEpicGroup(a.Source, b.Source)
	case a.Source.Tracker == "gitlab-epic" && b.Source.Tracker == "gitlab":
		return gitLabProjectWithinEpicGroup(b.Source, a.Source)
	default:
		return false
	}
}

func pairScopeDescription(a routeSource, b routeSource) string {
	if a.Source.Tracker == b.Source.Tracker {
		switch a.Source.Tracker {
		case "linear":
			return fmt.Sprintf("Linear project %q", linearScopeLabel(a.Source))
		case "gitlab":
			return fmt.Sprintf("GitLab project %q", strings.TrimSpace(a.Source.Connection.Project))
		case "gitlab-epic":
			return fmt.Sprintf("GitLab group %q", strings.TrimSpace(a.Source.Connection.GroupPath()))
		}
	}

	if a.Source.Tracker == "gitlab" && b.Source.Tracker == "gitlab-epic" {
		return fmt.Sprintf("GitLab project %q and gitlab-epic group %q", strings.TrimSpace(a.Source.Connection.Project), strings.TrimSpace(b.Source.Connection.GroupPath()))
	}
	if a.Source.Tracker == "gitlab-epic" && b.Source.Tracker == "gitlab" {
		return fmt.Sprintf("GitLab group %q and project %q", strings.TrimSpace(a.Source.Connection.GroupPath()), strings.TrimSpace(b.Source.Connection.Project))
	}
	return "tracker scope"
}

func combinedRouteNotes(a routeSource, b routeSource) string {
	notes := make([]string, 0, 4)
	for _, route := range []routeSource{a, b} {
		if route.Intake.IIDsIgnored {
			notes = append(notes, fmt.Sprintf("source %q sets issue filter IIDs, but current intake matching ignores IIDs", route.Source.Name))
		}
		if route.Intake.GitLabEpicBroad {
			notes = append(notes, fmt.Sprintf("source %q is gitlab-epic and its linked-issue intake does not inherit filter.labels; only issue_filter.labels constrain child issues", route.Source.Name))
		}
	}
	return strings.Join(dedupeStrings(notes), ". ") + sentenceTerminator(notes)
}

func sentenceTerminator(values []string) string {
	if len(values) == 0 {
		return ""
	}
	return "."
}

func sameLinearProject(a SourceConfig, b SourceConfig) bool {
	if normalizeBaseURL(a.Connection.BaseURL) != normalizeBaseURL(b.Connection.BaseURL) {
		return false
	}
	return hasStringIntersection(linearProjectHints(a), linearProjectHints(b))
}

func sameGitLabProject(a SourceConfig, b SourceConfig) bool {
	return normalizeBaseURL(a.Connection.BaseURL) == normalizeBaseURL(b.Connection.BaseURL) &&
		strings.EqualFold(strings.TrimSpace(a.Connection.Project), strings.TrimSpace(b.Connection.Project))
}

func sameGitLabGroup(a SourceConfig, b SourceConfig) bool {
	return normalizeBaseURL(a.Connection.BaseURL) == normalizeBaseURL(b.Connection.BaseURL) &&
		strings.EqualFold(strings.TrimSpace(a.Connection.GroupPath()), strings.TrimSpace(b.Connection.GroupPath()))
}

func gitLabProjectWithinEpicGroup(projectSource SourceConfig, epicSource SourceConfig) bool {
	if normalizeBaseURL(projectSource.Connection.BaseURL) != normalizeBaseURL(epicSource.Connection.BaseURL) {
		return false
	}
	project := strings.ToLower(strings.TrimSpace(projectSource.Connection.Project))
	group := strings.ToLower(strings.TrimSpace(epicSource.Connection.GroupPath()))
	if project == "" || group == "" {
		return false
	}
	return project == group || strings.HasPrefix(project, group+"/")
}

func epicFiltersOverlap(a epicFilterSpec, b epicFilterSpec) bool {
	if len(a.States) > 0 && len(b.States) > 0 && !hasStringIntersection(a.States, b.States) {
		return false
	}
	if len(a.IIDs) > 0 && len(b.IIDs) > 0 && !hasIntIntersection(a.IIDs, b.IIDs) {
		return false
	}
	return true
}

func applyTransitionPreview(filter issueFilterSpec, transition *LifecycleTransition) issueFilterSpec {
	preview := issueFilterSpec{
		Labels:   append([]string(nil), filter.Labels...),
		States:   append([]string(nil), filter.States...),
		Assignee: filter.Assignee,
	}

	for _, label := range transition.RemoveLabels {
		preview.Labels = removeString(preview.Labels, strings.ToLower(strings.TrimSpace(label)))
	}
	for _, label := range transition.AddLabels {
		normalized := strings.ToLower(strings.TrimSpace(label))
		if normalized == "" {
			continue
		}
		if !slices.Contains(preview.Labels, normalized) {
			preview.Labels = append(preview.Labels, normalized)
		}
	}
	sort.Strings(preview.Labels)

	if state := strings.ToLower(strings.TrimSpace(transition.State)); state != "" {
		preview.States = []string{state}
	}

	return preview
}

func previewMayMatchFilter(preview issueFilterSpec, filter issueFilterSpec) bool {
	if !labelSetSubsumes(filter.Labels, preview.Labels) {
		return false
	}
	if preview.Assignee != "" && filter.Assignee != "" && preview.Assignee != filter.Assignee {
		return false
	}
	if len(preview.States) > 0 && len(filter.States) > 0 && !hasStringIntersection(preview.States, filter.States) {
		return false
	}
	return true
}

func normalizeBaseURL(raw string) string {
	raw = strings.TrimSpace(strings.ToLower(raw))
	return strings.TrimRight(raw, "/")
}

func normalizeDiagnosticPrefix(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "maestro"
	}
	return raw
}

func linearScopeLabel(source SourceConfig) string {
	hints := linearProjectHints(source)
	if len(hints) == 0 {
		return ""
	}
	return hints[0]
}

func linearProjectHints(source SourceConfig) []string {
	var hints []string
	if slug := parseLinearProjectSlug(source.ProjectURL); slug != "" {
		hints = append(hints, slug)
	}
	if project := strings.ToLower(strings.TrimSpace(source.Connection.Project)); project != "" {
		hints = append(hints, project)
	}
	return dedupeStrings(hints)
}

func parseLinearProjectSlug(raw string) string {
	raw = strings.TrimSpace(strings.ToLower(raw))
	if raw == "" {
		return ""
	}
	marker := "/project/"
	idx := strings.Index(raw, marker)
	if idx == -1 {
		return ""
	}
	slug := raw[idx+len(marker):]
	if slash := strings.IndexByte(slug, '/'); slash >= 0 {
		slug = slug[:slash]
	}
	return strings.TrimSpace(slug)
}

func normalizeStringList(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		normalized := strings.ToLower(strings.TrimSpace(value))
		if normalized == "" {
			continue
		}
		if !slices.Contains(out, normalized) {
			out = append(out, normalized)
		}
	}
	sort.Strings(out)
	return out
}

func normalizeIntList(values []int) []int {
	out := make([]int, 0, len(values))
	seen := make(map[int]struct{}, len(values))
	for _, value := range values {
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	sort.Ints(out)
	return out
}

func dedupeStrings(values []string) []string {
	out := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		if strings.TrimSpace(value) == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func labelSetSubsumes(broader []string, narrower []string) bool {
	for _, label := range broader {
		if !slices.Contains(narrower, label) {
			return false
		}
	}
	return true
}

func stateSetSubsumes(broader []string, narrower []string) bool {
	if len(broader) == 0 {
		return true
	}
	if len(narrower) == 0 {
		return false
	}
	for _, state := range narrower {
		if !slices.Contains(broader, state) {
			return false
		}
	}
	return true
}

func hasStringIntersection(a []string, b []string) bool {
	for _, left := range a {
		if slices.Contains(b, left) {
			return true
		}
	}
	return false
}

func hasIntIntersection(a []int, b []int) bool {
	for _, left := range a {
		if slices.Contains(b, left) {
			return true
		}
	}
	return false
}

func formatStrings(values []string) string {
	quoted := make([]string, 0, len(values))
	for _, value := range values {
		quoted = append(quoted, fmt.Sprintf("%q", value))
	}
	return "[" + strings.Join(quoted, ", ") + "]"
}

func removeString(values []string, target string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		if value == target {
			continue
		}
		out = append(out, value)
	}
	return out
}
