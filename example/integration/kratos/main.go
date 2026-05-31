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

package main

import (
	"context"
	"log"
	"time"

	"kratos/internal/biz"
	"kratos/internal/conf"
	"kratos/internal/server"
	"kratos/internal/service"

	klog "github.com/go-kratos/kratos/v2/log"
	"google.golang.org/protobuf/types/known/durationpb"
)

func main() {
	httpServer := server.NewHTTPServer(
		&conf.Server{Http: &conf.Server_HTTP{Network: "tcp", Addr: ":8000", Timeout: durationpb.New(120 * time.Second)}},
		service.NewChatService(biz.NewChatUsecase()),
		klog.DefaultLogger,
	)
	if err := httpServer.Start(context.Background()); err != nil {
		log.Fatalf("failed to run server: %v", err)
	}
}
