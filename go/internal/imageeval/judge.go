package imageeval

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// JudgeInput 是一个槽位送给多模态评委的材料。金标可为 nil。
type JudgeInput struct {
	ImageType      string
	TypeJob        string
	ReferencePaths []string
	GoldPath       string
	WorkbenchPath  string
	NaivePath      string
}

// JudgeClient 给一个槽位打四维分。测试注入假实现；live 走视觉模型。
type JudgeClient interface {
	Judge(ctx context.Context, in JudgeInput) (SlotJudgement, error)
}

// GateSlot 按计划闸门判定一个槽位。评委分数需已填。
func GateSlot(slot SlotJudgement) SlotJudgement {
	slot.FailReasons = nil
	if err := ValidateScores(slot.Workbench); err != nil {
		slot.FailReasons = append(slot.FailReasons, err.Error())
	}
	if err := ValidateScores(slot.Naive); err != nil {
		slot.FailReasons = append(slot.FailReasons, err.Error())
	}
	if slot.Gold != nil {
		if err := ValidateScores(*slot.Gold); err != nil {
			slot.FailReasons = append(slot.FailReasons, err.Error())
		}
	}
	wb := slot.Workbench.Mean()
	nv := slot.Naive.Mean()
	slot.VsNaive = cmpLabel(wb, nv)
	if wb <= nv {
		slot.FailReasons = append(slot.FailReasons, "workbench mean must beat naive")
	}
	if slot.Workbench.Fidelity < slot.Naive.Fidelity {
		slot.FailReasons = append(slot.FailReasons, "workbench fidelity below naive")
	}
	if slot.Gold != nil {
		gd := slot.Gold.Mean()
		slot.VsGold = cmpLabel(wb, gd)
		if wb < gd {
			slot.FailReasons = append(slot.FailReasons, "workbench mean below gold")
		}
		if slot.Workbench.Fidelity < slot.Gold.Fidelity {
			slot.FailReasons = append(slot.FailReasons, "workbench fidelity below gold")
		}
	} else {
		slot.VsGold = "none"
	}
	slot.Passed = len(slot.FailReasons) == 0
	return slot
}

func cmpLabel(a, b float64) string {
	if a > b {
		return "win"
	}
	if a < b {
		return "lose"
	}
	return "tie"
}

// ParseJudgeJSON 把评委模型输出收成 Scores 三组。
func ParseJudgeJSON(raw string) (workbench Scores, gold *Scores, naive Scores, notes string, err error) {
	raw = strings.TrimSpace(raw)
	if i := strings.Index(raw, "{"); i >= 0 {
		if j := strings.LastIndex(raw, "}"); j > i {
			raw = raw[i : j+1]
		}
	}
	var payload struct {
		Workbench Scores  `json:"workbench"`
		Gold      *Scores `json:"gold"`
		Naive     Scores  `json:"naive"`
		Notes     string  `json:"notes"`
	}
	if err = json.Unmarshal([]byte(raw), &payload); err != nil {
		return Scores{}, nil, Scores{}, "", fmt.Errorf("judge json: %w", err)
	}
	if err = ValidateScores(payload.Workbench); err != nil {
		return Scores{}, nil, Scores{}, "", err
	}
	if err = ValidateScores(payload.Naive); err != nil {
		return Scores{}, nil, Scores{}, "", err
	}
	if payload.Gold != nil {
		if err = ValidateScores(*payload.Gold); err != nil {
			return Scores{}, nil, Scores{}, "", err
		}
	}
	return payload.Workbench, payload.Gold, payload.Naive, payload.Notes, nil
}

// JudgeSystemPrompt 是视觉评委系统提示。输出必须是 JSON。
const JudgeSystemPrompt = `你是电商套图评委。只根据给定图片打分，不要编造没看到的细节。
对工作台成片、对家金标（若有）、一句直调成片分别打 1 到 5 分，维度：
- fidelity 保真：外形、颜色、材质、可见标识相对参考图
- fit 适配：是否履行该图种职责
- utility 实用：能否当详情图（可认、可排版、该有字则中文可读）
- aesthetics 美观：商业完成度
只输出 JSON：{"workbench":{"fidelity":n,"fit":n,"utility":n,"aesthetics":n},"gold":null或同样对象,"naive":同样对象,"notes":"短优缺"}。`
