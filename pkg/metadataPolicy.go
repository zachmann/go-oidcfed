package pkg

import (
	"fmt"

	"github.com/pkg/errors"
)

type MetadataPolicies struct {
	OpenIDProvider           MetadataPolicy `json:"openid_provider,omitempty"`
	RelyingParty             MetadataPolicy `json:"openid_relying_party,omitempty"`
	OAuthAuthorizationServer MetadataPolicy `json:"oauth_authorization_server,omitempty"`
	OAuthClient              MetadataPolicy `json:"oauth_client,omitempty"`
	OAuthProtectedResource   MetadataPolicy `json:"oauth_resource,omitempty"`
	FederationEntity         MetadataPolicy `json:"federation_entity,omitempty"`
}

type MetadataPolicy map[string]MetadataPolicyEntry
type MetadataPolicyEntry map[PolicyOperatorName]any
type PolicyOperatorName string

func (p MetadataPolicy) Verify(pathInfo string) error {
	for k, v := range p {
		if err := v.Verify(fmt.Sprintf("%s.%s", pathInfo, k)); err != nil {
			return err
		}
	}
	return nil
}

// MergeMetadataPolicies combines multiples MetadataPolicies from a chain into a single one
func MergeMetadataPolicies(policies ...*MetadataPolicies) (*MetadataPolicies, error) {
	opEntries := make([]MetadataPolicy, 0)
	rpEntries := make([]MetadataPolicy, 0)
	asEntries := make([]MetadataPolicy, 0)
	ocEntries := make([]MetadataPolicy, 0)
	prEntries := make([]MetadataPolicy, 0)
	feEntries := make([]MetadataPolicy, 0)
	for _, p := range policies {
		if p == nil {
			continue
		}
		opEntries = append(opEntries, p.OpenIDProvider)
		rpEntries = append(rpEntries, p.RelyingParty)
		asEntries = append(asEntries, p.OAuthAuthorizationServer)
		ocEntries = append(ocEntries, p.OAuthClient)
		prEntries = append(prEntries, p.OAuthProtectedResource)
		feEntries = append(feEntries, p.FederationEntity)
	}
	op, err := CombineMetadataPolicy("openid_provider", opEntries...)
	if err != nil {
		return nil, err
	}
	rp, err := CombineMetadataPolicy("openid_relying_party", rpEntries...)
	if err != nil {
		return nil, err
	}
	as, err := CombineMetadataPolicy("oauth_authorization_server", asEntries...)
	if err != nil {
		return nil, err
	}
	c, err := CombineMetadataPolicy("oauth_client", ocEntries...)
	if err != nil {
		return nil, err
	}
	pr, err := CombineMetadataPolicy("oauth_resource", prEntries...)
	if err != nil {
		return nil, err
	}
	fed, err := CombineMetadataPolicy("federation_entity", feEntries...)
	if err != nil {
		return nil, err
	}
	return &MetadataPolicies{
		OpenIDProvider:           op,
		RelyingParty:             rp,
		OAuthAuthorizationServer: as,
		OAuthClient:              c,
		OAuthProtectedResource:   pr,
		FederationEntity:         fed,
	}, nil
}

// CombineMetadataPolicy combines multiples MetadataPolicy into a single MetadataPolicy,
// at each step verifying that the result is valid
func CombineMetadataPolicy(pathInfo string, policies ...MetadataPolicy) (MetadataPolicy, error) {
	if len(policies) == 0 {
		return nil, nil
	}
	var err error
	out := policies[0]
	for i := 1; i < len(policies); i++ {
		out, err = combineMetadataPolicy(out, policies[i], pathInfo)
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}

// combineMetadataPolicy combines two MetadataPolicy and verifies that the resulting MetadataPolicy is valid
func combineMetadataPolicy(parent, sub MetadataPolicy, pathInfo string) (MetadataPolicy, error) {
	if len(sub) == 0 {
		return parent, nil
	}
	if len(parent) == 0 {
		return sub, nil
	}
	out := make(MetadataPolicy)
	for k, pv := range parent {
		sv, sFound := sub[k]
		if !sFound {
			out[k] = pv
			continue
		}
		combined, err := mergeMetadataPolicyEntries(pv, sv, fmt.Sprintf("%s.%s", pathInfo, k))
		if err != nil {
			return nil, err
		}
		out[k] = combined
	}
	for k, sv := range sub {
		if _, found := out[k]; found {
			continue
		}
		out[k] = sv
	}
	return out, out.Verify(pathInfo)
}

func mergeMetadataPolicyEntries(a, b MetadataPolicyEntry, pathInfo string) (MetadataPolicyEntry, error) {
	out := make(MetadataPolicyEntry)
	for op, av := range a {
		bv, bFound := b[op]
		if !bFound {
			out[op] = av
			continue
		}
		operator, ok := operators[op]
		if !ok {
			return nil, errors.Errorf("unknown policy operator '%s'; cannot combine these policies", op)
		}
		combined, err := operator.Merge(av, bv, pathInfo)
		if err != nil {
			return nil, err
		}
		out[op] = combined
	}
	for op, bv := range b {
		if _, found := out[op]; found {
			continue
		}
		out[op] = bv
	}
	return out, nil
}

func (p MetadataPolicyEntry) Verify(pathInfo string) error {
	for _, v := range policyVerifiers {
		if err := v(p, pathInfo); err != nil {
			return err
		}
	}
	return nil
}

func (p MetadataPolicyEntry) ApplyTo(value any, pathInfo string) (any, error) {
	var err error
	doTheChecks := false
	essentialV, ok := p[PolicyOperatorEssential]
	essential := false
	if ok {
		essential, _ = essentialV.(bool)
	}
	for i := 0; i < 2; i++ { // we go twice through the policies; first only applying modifiers, then the checks
		for policyName, policyValue := range p {
			operator, found := operators[policyName]
			if !found {
				return value, errors.Errorf("unsupported policy operator '%s' in '%s'", policyName, pathInfo)
			}
			if operator.IsModifier() == doTheChecks {
				continue
			}
			value, err = operator.Apply(value, policyValue, essential, pathInfo)
			if err != nil {
				return value, err
			}
		}
		doTheChecks = true
	}
	return value, nil
}
