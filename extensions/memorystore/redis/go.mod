module github.com/yuluo-yx/agentscope-go/extensions/memorystore/redis

go 1.26.4

require (
	github.com/redis/go-redis/v9 v9.21.0
	github.com/yuluo-yx/agentscope-go v0.0.0
)

require (
	github.com/alicebob/miniredis/v2 v2.38.0 // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/pmezard/go-difflib v1.0.1-0.20181226105442-5d4384ee4fb2 // indirect
	github.com/yuin/gopher-lua v1.1.1 // indirect
	go.uber.org/atomic v1.11.0 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
	mvdan.cc/sh/v3 v3.13.1 // indirect
)

replace github.com/yuluo-yx/agentscope-go => ../../..
