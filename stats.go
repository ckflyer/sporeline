package main

import (
	"sort"
	"time"
)

// What the log can tell you once there is a season of it. Everything
// here is derived on the fly; nothing extra is stored.

type kindStat struct {
	Kind   Kind
	Total  int
	OnHand int
	Gone   int
}

// Contamination is the only loss worth measuring: throwing something out
// or using it up is just the end of its life, not a failure.
type contamStat struct {
	Label   string
	Total   int
	Lost    int
	Percent int
}

type strainStat struct {
	Name        string
	Total       int
	Lost        int
	LossPercent int
	Grows       int // grows with at least one weight
	wet, dry    float64
	wetN, dryN  int
	AvgWet      float64
	AvgDry      float64
}

type statsPage struct {
	Total, OnHand, Gone   int
	Strains, SpeciesCount int
	Pictures, Recipes     int
	Kinds                 []kindStat
	ByStage               []contamStat
	Contaminated          int
	ContamRate            int
	// Wet and dry weights are never added together: a dry flush weighs
	// roughly a tenth of a wet one, so mixing them would mean nothing.
	TotalWet, TotalDry    float64
	WetGrows, DryGrows    int
	AvgWet, AvgDry        float64
	UnmarkedGrows         int // grows holding an old weight not yet marked wet or dry
	BestGrow              *Component
	BestGrams             float64
	BestState             string // "dry" or "wet"
	Strain                []strainStat
	OldestOnHand          *Component
	OldestAge             int
	Enough                bool
}

func buildStats() statsPage {
	all := store.All()
	s := statsPage{Recipes: len(store.Recipes())}
	strains := map[string]bool{}
	species := map[string]bool{}
	reasons := map[string]int{}
	contamByKind := map[string]int{}
	byStrain := map[string]*strainStat{}

	for _, k := range Kinds {
		s.Kinds = append(s.Kinds, kindStat{Kind: k})
	}

	for _, c := range all {
		s.Total++
		s.Pictures += len(c.Pics)
		if c.Strain != "" {
			strains[c.Strain] = true
		}
		if c.Species != "" {
			species[c.Species] = true
		}
		name := c.Title()
		st, ok := byStrain[name]
		if !ok {
			st = &strainStat{Name: name}
			byStrain[name] = st
		}
		st.Total++

		if c.Gone {
			s.Gone++
			reasons[c.GoneReason]++
			if c.GoneReason == "contaminated" {
				st.Lost++
				contamByKind[c.Kind]++
			}
		} else {
			s.OnHand++
			if age := daysSince(c.Created); age > s.OldestAge {
				s.OldestAge, s.OldestOnHand = age, c
			}
		}
		for i := range s.Kinds {
			if s.Kinds[i].Kind.Key == c.Kind {
				s.Kinds[i].Total++
				if c.Gone {
					s.Kinds[i].Gone++
				} else {
					s.Kinds[i].OnHand++
				}
			}
		}
		if c.Kind == "grow" {
			wet, dry := c.TotalWet(), c.TotalDry()
			if wet > 0 {
				s.TotalWet += wet
				s.WetGrows++
				st.wet += wet
				st.wetN++
			}
			if dry > 0 {
				s.TotalDry += dry
				s.DryGrows++
				st.dry += dry
				st.dryN++
			}
			if wet > 0 || dry > 0 {
				st.Grows++
			}
			if c.TotalUnmarked() > 0 {
				s.UnmarkedGrows++
			}
		}
	}
	// Best grow goes by dry weight when there is any, since that is what
	// is left in the jar; otherwise by wet.
	s.BestState = "wet"
	if s.DryGrows > 0 {
		s.BestState = "dry"
	}
	for _, c := range all {
		if c.Kind != "grow" {
			continue
		}
		g := c.TotalWet()
		if s.BestState == "dry" {
			g = c.TotalDry()
		}
		if g > s.BestGrams {
			s.BestGrow, s.BestGrams = c, g
		}
	}

	s.Strains, s.SpeciesCount = len(strains), len(species)
	if s.WetGrows > 0 {
		s.AvgWet = s.TotalWet / float64(s.WetGrows)
	}
	if s.DryGrows > 0 {
		s.AvgDry = s.TotalDry / float64(s.DryGrows)
	}
	s.Contaminated = reasons["contaminated"]
	if s.Total > 0 {
		s.ContamRate = s.Contaminated * 100 / s.Total
	}
	// Where in the process things go wrong is the useful cut.
	for _, k := range s.Kinds {
		if k.Total == 0 {
			continue
		}
		lost := contamByKind[k.Kind.Key]
		s.ByStage = append(s.ByStage, contamStat{
			Label: k.Kind.Plural, Total: k.Total, Lost: lost,
			Percent: lost * 100 / k.Total,
		})
	}

	for _, st := range byStrain {
		if st.Total > 0 {
			st.LossPercent = st.Lost * 100 / st.Total
		}
		if st.wetN > 0 {
			st.AvgWet = st.wet / float64(st.wetN)
		}
		if st.dryN > 0 {
			st.AvgDry = st.dry / float64(st.dryN)
		}
		s.Strain = append(s.Strain, *st)
	}
	sort.Slice(s.Strain, func(i, j int) bool {
		if s.Strain[i].Total != s.Strain[j].Total {
			return s.Strain[i].Total > s.Strain[j].Total
		}
		return s.Strain[i].Name < s.Strain[j].Name
	})
	// Below a handful of entries these numbers say more about chance
	// than about your process, so the page says so.
	s.Enough = s.Total >= 10
	return s
}

func daysSince(date string) int {
	t, err := time.Parse("2006-01-02", date)
	if err != nil {
		return 0
	}
	return int(time.Since(t).Hours() / 24)
}
