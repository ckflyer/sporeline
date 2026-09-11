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
	Grows       int
	Yield       float64
	AvgYield    float64
}

type statsPage struct {
	Total, OnHand, Gone   int
	Strains, SpeciesCount int
	Pictures, Recipes     int
	Kinds                 []kindStat
	ByStage               []contamStat
	Contaminated          int
	ContamRate            int
	TotalYield            float64
	GrowsWithYield        int
	AvgYield              float64
	BestGrow              *Component
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
		if c.Kind == "grow" && c.Yield > 0 {
			s.TotalYield += c.Yield
			s.GrowsWithYield++
			st.Grows++
			st.Yield += c.Yield
			if s.BestGrow == nil || c.Yield > s.BestGrow.Yield {
				s.BestGrow = c
			}
		}
	}

	s.Strains, s.SpeciesCount = len(strains), len(species)
	if s.GrowsWithYield > 0 {
		s.AvgYield = s.TotalYield / float64(s.GrowsWithYield)
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
		if st.Grows > 0 {
			st.AvgYield = st.Yield / float64(st.Grows)
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
