package operator

import egressv1alpha1 "github.com/egressfox-io/egressfox/api/v1alpha1"

// ResolveProfile returns one complete probe/selection pair. The legacy fields
// always own the reserved default profile and are never merged with named ones.
func ResolveProfile(pool *egressv1alpha1.ProxyPool, name string) (egressv1alpha1.ProbeSpec, egressv1alpha1.SelectionSpec, error) {
	if pool == nil || len(pool.Spec.Profiles) > 8 {
		return egressv1alpha1.ProbeSpec{}, egressv1alpha1.SelectionSpec{}, pipelineFailure("profile_invalid")
	}
	if name == "" || name == "default" {
		name = "default"
	} else if !validProfileName(name) {
		return egressv1alpha1.ProbeSpec{}, egressv1alpha1.SelectionSpec{}, pipelineFailure("profile_invalid")
	}
	seen := map[string]bool{}
	var selected *egressv1alpha1.TargetProfile
	for _, profile := range pool.Spec.Profiles {
		if !validProfileName(profile.Name) || profile.Name == "default" || seen[profile.Name] {
			return egressv1alpha1.ProbeSpec{}, egressv1alpha1.SelectionSpec{}, pipelineFailure("profile_invalid")
		}
		if profile.Probe.TargetSecretRef.Name == "" || profile.Probe.TargetSecretRef.Key == "" ||
			profile.Selection.TopN < 0 || profile.Selection.TopN > 10000 ||
			profile.Selection.Strategy != "" && profile.Selection.Strategy != egressv1alpha1.SelectionAdaptive && profile.Selection.Strategy != egressv1alpha1.SelectionStatic && profile.Selection.Strategy != egressv1alpha1.SelectionLowestLatency {
			return egressv1alpha1.ProbeSpec{}, egressv1alpha1.SelectionSpec{}, pipelineFailure("profile_invalid")
		}
		seen[profile.Name] = true
		if profile.Name == name {
			copy := profile
			selected = &copy
		}
	}
	if selected != nil {
		return selected.Probe, selected.Selection, nil
	}
	if name == "default" {
		return pool.Spec.Probe, pool.Spec.Selection, nil
	}
	return egressv1alpha1.ProbeSpec{}, egressv1alpha1.SelectionSpec{}, pipelineFailure("profile_missing")
}

func validProfileName(name string) bool {
	if len(name) < 1 || len(name) > 63 || name[0] < 'a' || name[0] > 'z' || name[len(name)-1] == '-' {
		return false
	}
	for _, r := range name[1:] {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-' {
			continue
		}
		return false
	}
	return true
}
