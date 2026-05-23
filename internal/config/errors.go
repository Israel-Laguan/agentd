package config

import "errors"

var (
	ErrConfigRead       = errors.New("config read failed")
	ErrDirsNotWritable  = errors.New("directory not writable")
	ErrLLMWarmup        = errors.New("LLM warmup failed")
	ErrNoLLMProviders   = errors.New("no LLM providers available")
)
