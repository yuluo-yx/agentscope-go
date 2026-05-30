// Copyright 20\d\d AgentScope Go
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

package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/yuluo-yx/agentscope-go/tool/skill"
)

func main() {
	loader := skill.NewLocalLoader("resources", skill.WithScanSubdirs(true))
	skills, err := loader.ListSkills(context.Background())
	if err != nil {
		panic(err)
	}

	names := make([]string, 0, len(skills))
	for _, loaded := range skills {
		names = append(names, loaded.Name)
	}
	fmt.Printf("skills=%d names=%s first_body_len=%d\n", len(skills), strings.Join(names, ","), len(skills[0].Markdown))
}
