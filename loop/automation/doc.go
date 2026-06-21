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

// Package automation 提供事件驱动的 Loop Engineering 外层编排能力。
//
// 本包位于 loop 包之上。loop 包控制一次 Agent run，本包负责决定哪个
// 通用事件触发该 run、事件如何映射为 Agent 输入、并发、预算和估算成本如何受控、
// 目标如何跨 run 续跑，以及 event、run、report 如何审计。LoopTemplate 用于描述
// 可复用 loop 配置和项目知识引用，但不绑定具体插件格式。成本估算只通过通用
// CostEstimator 接入，模型价格表和账单语义由应用侧 adapter 提供。
package automation
