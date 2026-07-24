/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
package model

import (
	"errors"
	"strings"

	"github.com/QuantumNous/new-api/common"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const monitorQuestionSeedMarkerKey = "ChannelMonitorQuestionsSeededV1"

var defaultMonitorQuestions = []string{
	"请用一句话说明 HTTP 404 通常表示什么。",
	"请给出一个 Python 列表去重的简单方法。",
	"请用一句话解释什么是 API 健康检查。",
	"一句话解释Superpowers是干嘛用的。",
	"Tailscale是干嘛用的，一句话简要说明。",
	"帮我取网名，偏正义，返回3个即可。",
	"帮我翻译codex的指令：/permissions  choose what Codex is allowed to do。",
	"帮我翻译codex的指令：/skills  use skills to improve how Codex performs specific tasks。",
	"帮我翻译codex的指令：/compact  summarize conversation to prevent hitting the context limit。",
	"帮我翻译codex的指令：/goal  set or view the goal for a long-running task",
	"帮我翻译codex的指令：/agent  switch the active agent thread。",
	"帮我翻译codex的指令：/statusline  configure which items appear in the status line。",
	"帮我翻译codex的指令：/plugins  browse plugins。",
	"帮我翻译codex的指令：//feedback  send logs to maintainers。",
	"帮我翻译codex的指令：/personality  choose a communication style for Codex。",
	"什么是变量？用一句话说清楚。",
	"简单解释一下函数为什么能减少重复代码。",
	"数组和链表有什么区别？简要回答。",
	"请用一个生活类比说明什么是队列。",
	"什么是栈？一句话说明它的特点。",
	"简单说说 Git commit 是做什么的。",
	"为什么要写注释？用一句话回答。",
	"什么情况下不适合写太多注释？简要说明。",
	"请简单解释一下什么是单元测试。",
	"接口和实现有什么区别？简要回答。",
	"什么是 HTTP 请求方法 GET？一句话说明。",
	"POST 请求通常用来做什么？简单回答。",
	"请用一句话解释 JSON 是什么。",
	"什么是环境变量？简要说明。",
	"为什么不要把密钥写死在代码里？一句话回答。",
	"简单解释一下什么是异常处理。",
	"try except 的作用是什么？简要说明。",
	"什么是日志？一句话说明它在排查问题中的作用。",
	"请简单说明 debug 和 release 的区别。",
	"什么是依赖库？简单回答。",
	"为什么项目需要 README？一句话说明。",
	"请简要解释什么是代码格式化。",
	"Lint 工具一般用来检查什么？简单说明。",
	"什么是数据库索引？用一句话解释。",
	"为什么索引不一定越多越好？简要回答。",
	"简单说明 SQL 里的 WHERE 是干嘛的。",
	"什么是主键？一句话回答。",
	"请简要说明前端和后端的区别。",
	"浏览器缓存有什么作用？简单说说。",
	"什么是跨域问题？一句话说明。",
	"API 返回 500 一般代表什么？简单回答。",
	"什么是幂等？用一个简单例子说明。",
	"请简要说明重构是什么意思。",
	"为什么重构前最好有测试？一句话回答。",
	"什么是代码可读性？简单解释。",
	"请说说命名为什么重要，简短回答。",
	"什么是递归？用一句话说明。",
	"递归为什么需要结束条件？简单解释。",
	"什么是时间复杂度？简要说明。",
	"O(n) 大概表示什么？简单回答。",
	"什么是内存泄漏？一句话解释。",
	"简单说明线程和进程的区别。",
	"什么是并发？用一句话回答。",
	"锁通常用来解决什么问题？简要说明。",
	"请简单解释一下什么是 REST API。",
	"什么是版本号？一句话说明它的意义。",
	"为什么要做代码评审？简单回答。",
	"什么是技术债？简要说明。",
	"请用一句话解释 CI 是什么。",
	"部署和发布有什么区别？简单说说。",
}

// InitializeMonitorQuestions imports the bundled probe questions exactly once.
// Existing user-created questions are preserved, and exact duplicates are
// skipped. The durable marker prevents deleted seed questions from being
// silently recreated on later restarts.
func InitializeMonitorQuestions() error {
	if DB == nil {
		return errors.New("database is not initialized")
	}

	return DB.Transaction(func(tx *gorm.DB) error {
		claimValue := common.GetRandomString(32)
		marker := Option{Key: monitorQuestionSeedMarkerKey, Value: claimValue}
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&marker).Error; err != nil {
			return err
		}

		var storedMarker Option
		if err := tx.Where("key = ?", monitorQuestionSeedMarkerKey).First(&storedMarker).Error; err != nil {
			return err
		}
		if storedMarker.Value != claimValue {
			return nil
		}

		var existingContents []string
		if err := tx.Model(&MonitorQuestion{}).Pluck("content", &existingContents).Error; err != nil {
			return err
		}
		existing := make(map[string]struct{}, len(existingContents))
		for _, content := range existingContents {
			existing[strings.TrimSpace(content)] = struct{}{}
		}

		now := common.GetTimestamp()
		questions := make([]MonitorQuestion, 0, len(defaultMonitorQuestions))
		for _, content := range defaultMonitorQuestions {
			content = strings.TrimSpace(content)
			if content == "" {
				continue
			}
			if _, exists := existing[content]; exists {
				continue
			}
			existing[content] = struct{}{}
			questions = append(questions, MonitorQuestion{
				Content:     content,
				CreatedTime: now,
				UpdatedTime: now,
			})
		}
		if len(questions) > 0 {
			if err := tx.CreateInBatches(&questions, 100).Error; err != nil {
				return err
			}
		}

		return tx.Model(&Option{}).
			Where("key = ? AND value = ?", monitorQuestionSeedMarkerKey, claimValue).
			Update("value", "1").Error
	})
}
