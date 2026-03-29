package agent

import "regexp"

// sensitivePatterns is the list of regexes used to mask credentials in log output.
// Each pattern must capture the sensitive value in group 1 so it can be replaced
// with "***" while keeping the surrounding context intact.
var sensitivePatterns = []*regexp.Regexp{
	// OpenAI / Anthropic-style secret keys: sk-<value>
	regexp.MustCompile(`(?i)(sk-)[^\s"'&]{8,}`),
	// Google / Gemini API keys: AIza<value>
	regexp.MustCompile(`(AIza)[^\s"'&]{8,}`),
	// Bearer tokens in Authorization headers
	regexp.MustCompile(`(?i)(Bearer\s+)[^\s"'&]{4,}`),
	// URL query parameters: key=, api_key=, token=, access_token=, secret=
	regexp.MustCompile(`(?i)(\b(?:key|api[_-]key|token|access[_-]token|secret)=)[^\s"'&]{4,}`),
}

// SanitizeLog masks known sensitive patterns (API keys, tokens, bearer credentials)
// in s and returns the sanitized string. It is safe to call on any log message.
func SanitizeLog(s string) string {
	for _, re := range sensitivePatterns {
		s = re.ReplaceAllStringFunc(s, func(match string) string {
			// Find the prefix (captured group 1 equivalent) by re-matching.
			sub := re.FindStringSubmatchIndex(match)
			if len(sub) < 4 {
				return match
			}
			// sub[2]..sub[3] is the position of the first capture group inside match.
			prefix := match[sub[2]:sub[3]]
			return prefix + "***"
		})
	}
	return s
}
