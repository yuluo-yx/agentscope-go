// Copyright The AgentScope Go Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package utils

import (
	"strings"
	"sync"

	"github.com/google/uuid"
)

var (
	idFactoryMu sync.RWMutex
	idFactory   = defaultIDFactory
)

// SetIDFactory 设置 NewID 使用的全局 ID 工厂；传入 nil 会恢复默认 UUID hex 生成器。
func SetIDFactory(factory func() string) {
	idFactoryMu.Lock()
	defer idFactoryMu.Unlock()

	if factory == nil {
		idFactory = defaultIDFactory
		return
	}
	idFactory = factory
}

// ResetIDFactory 恢复默认的 Python 兼容 UUID hex 生成器。
func ResetIDFactory() {
	SetIDFactory(nil)
}

// NewID returns a Python-compatible UUID hex identifier without dashes.
func NewID() string {
	idFactoryMu.RLock()
	factory := idFactory
	idFactoryMu.RUnlock()

	return factory()
}

func defaultIDFactory() string {
	return strings.ReplaceAll(uuid.NewString(), "-", "")
}
