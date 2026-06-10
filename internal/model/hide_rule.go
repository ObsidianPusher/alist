package model

import (
	"strings"

	"github.com/dlclark/regexp2"
	log "github.com/sirupsen/logrus"
)

func CompileHidePatterns(hides string) []*regexp2.Regexp {
	rules := strings.Split(hides, "\n")
	regs := make([]*regexp2.Regexp, 0, len(rules))
	for _, rule := range rules {
		rule = strings.TrimSpace(rule)
		if rule == "" {
			continue
		}
		reg, err := regexp2.Compile(rule, regexp2.None)
		if err != nil {
			log.WithError(err).Warnf("skip invalid hide rule: %q", rule)
			continue
		}
		regs = append(regs, reg)
	}
	return regs
}
