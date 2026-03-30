# Spec: Gemini API Key Migration to Authorization Header

## Overview

The Gemini API key must be passed via the `x-goog-api-key` HTTP request header instead of as a URL query parameter (`?key=<apiKey>`).

## Motivation

Passing secrets as URL query parameters exposes them in:
- Server-side and proxy access logs
- Browser history
- Error messages that include the full URL

Using an HTTP header (`x-goog-api-key`) keeps the API key out of the URL and reduces the risk of accidental exposure.

## Behavior

### Before (insecure)

The `callAPIWithURL` function in `internal/agent/gemini_api.go` appends the API key to the request URL:

```
https://generativelanguage.googleapis.com/v1beta/models/<model>:generateContent?key=<apiKey>
```

### After (secure)

The `callAPIWithURL` function sends the API key as an HTTP header:

```
POST https://generativelanguage.googleapis.com/v1beta/models/<model>:generateContent
x-goog-api-key: <apiKey>
```

The URL must not contain the `key=` query parameter.

## Implementation

- File: `internal/agent/gemini_api.go`
- Function: `callAPIWithURL`
- Change: Remove the query parameter appending logic; add `req.Header.Set("x-goog-api-key", apiKey)` after creating the request.

## Edge Cases

- The URL must not contain `?key=` or `&key=` after the change.
- The `x-goog-api-key` header must always be set when an API key is present.
- Tests must verify the header is present and the query parameter is absent.
