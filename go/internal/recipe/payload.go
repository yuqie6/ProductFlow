package recipe

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/yuqie6/productflow/internal/graph"
	"github.com/yuqie6/productflow/internal/platform/apperr"
	"github.com/yuqie6/productflow/internal/platform/canonjson"
)

const (
	schemaVersion     = 3
	maxNodes          = 128
	maxEdges          = 256
	maxGroups         = 32
	maxJSONBytes      = 512 * 1024
	maxKeyLen         = 80
	maxTitleLen       = 255
	maxRefLen         = 500
	kindWorkflow      = "workflow_recipe"
	kindFragment      = "recipe_fragment"
	originUser        = "user"
	originOfficial    = "official"
	sourceUserExtract = "user_extract"
	SourceWorkflow    = "workflow"
	SourceGroup       = "group"
	SourceSelection   = "selection"
	ModeCreate        = "create"
	ModeMerge         = "merge"
)

var (
	recipeKeyRE        = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]*$`)
	identityConfigKeys = map[string]struct{}{
		"source_product_id":        {},
		"fact_set_version_id":      {},
		"visual_system_version_id": {},
	}
	strippedPromptKeys = map[string]struct{}{
		"images":             {},
		"fact_keys":          {},
		"evidence_asset_ids": {},
		"prompt_plan_key":    {},
		"image_plan_key":     {},
		"prompt_plan_keys":   {},
		"image_plan_keys":    {},
		"document_origin":    {},
		"visual_overrides":   {},
	}
	forbiddenPlanKeys = map[string]struct{}{
		"prompt_plan_key":  {},
		"image_plan_key":   {},
		"prompt_plan_keys": {},
		"image_plan_keys":  {},
	}
	validNodeTypes = map[graph.NodeType]struct{}{
		graph.NodeProductSource:    {},
		graph.NodeImageAsset:       {},
		graph.NodeCreativeBrief:    {},
		graph.NodeVisualSystem:     {},
		graph.NodePromptGeneration: {},
		graph.NodeImageGeneration:  {},
	}
	validDataTypes = map[graph.EdgeDataType]struct{}{
		graph.DataProductFacts:  {},
		graph.DataImageAsset:    {},
		graph.DataCreativeBrief: {},
		graph.DataVisualSystem:  {},
		graph.DataPrompt:        {},
	}
	validRoles = map[graph.EdgeRole]struct{}{
		graph.RoleFacts:          {},
		graph.RoleReference:      {},
		graph.RoleBrief:          {},
		graph.RoleVisualGuidance: {},
		graph.RolePrompt:         {},
	}
)

type Payload struct {
	SchemaVersion int
	Nodes         []PayloadNode
	Edges         []PayloadEdge
	Groups        []PayloadGroup
}

type PayloadNode struct {
	Key       string
	NodeType  graph.NodeType
	Title     string
	PositionX int
	PositionY int
	GroupKey  *string
	Config    map[string]any
}

type PayloadEdge struct {
	Key           string
	SourceNodeKey string
	TargetNodeKey string
	DataType      graph.EdgeDataType
	Role          graph.EdgeRole
	Order         int
}

type PayloadGroup struct {
	Key        string
	Title      string
	MemberKeys []string
}

type payloadWire struct {
	SchemaVersion int         `json:"schema_version"`
	Nodes         []nodeWire  `json:"nodes"`
	Edges         []edgeWire  `json:"edges"`
	Groups        []groupWire `json:"groups"`
}

type nodeWire struct {
	Key       string         `json:"key"`
	NodeType  graph.NodeType `json:"node_type"`
	Title     string         `json:"title"`
	PositionX int            `json:"position_x"`
	PositionY int            `json:"position_y"`
	GroupKey  *string        `json:"group_key"`
	Config    map[string]any `json:"config"`
}

type edgeWire struct {
	Key           string             `json:"key"`
	SourceNodeKey string             `json:"source_node_key"`
	TargetNodeKey string             `json:"target_node_key"`
	DataType      graph.EdgeDataType `json:"data_type"`
	Role          graph.EdgeRole     `json:"role"`
	Order         int                `json:"order"`
}

type groupWire struct {
	Key        string   `json:"key"`
	Title      string   `json:"title"`
	MemberKeys []string `json:"member_keys"`
}

type governanceWire struct {
	ApplicableImageTypes []string `json:"applicable_image_types"`
	RequiredInputs       []string `json:"required_inputs"`
	DefaultResult        string   `json:"default_result"`
	Thumbnail            *string  `json:"thumbnail"`
	ProviderSample       *string  `json:"provider_sample"`
}

func payloadDict(p Payload) map[string]any {
	nodes := make([]any, 0, len(p.Nodes))
	for _, node := range p.Nodes {
		config := cloneValue(node.Config)
		if config == nil {
			config = map[string]any{}
		}
		var groupKey any
		if node.GroupKey != nil {
			groupKey = *node.GroupKey
		}
		nodes = append(nodes, map[string]any{
			"key":        node.Key,
			"node_type":  string(node.NodeType),
			"title":      node.Title,
			"position_x": node.PositionX,
			"position_y": node.PositionY,
			"group_key":  groupKey,
			"config":     config,
		})
	}
	edges := make([]any, 0, len(p.Edges))
	for _, edge := range p.Edges {
		edges = append(edges, map[string]any{
			"key":             edge.Key,
			"source_node_key": edge.SourceNodeKey,
			"target_node_key": edge.TargetNodeKey,
			"data_type":       string(edge.DataType),
			"role":            string(edge.Role),
			"order":           edge.Order,
		})
	}
	groups := make([]any, 0, len(p.Groups))
	for _, group := range p.Groups {
		members := group.MemberKeys
		if members == nil {
			members = []string{}
		}
		groups = append(groups, map[string]any{
			"key":         group.Key,
			"title":       group.Title,
			"member_keys": members,
		})
	}
	return map[string]any{
		"schema_version": schemaVersion,
		"nodes":          nodes,
		"edges":          edges,
		"groups":         groups,
	}
}

func payloadJSON(p Payload) ([]byte, error) {
	encoded, err := canonjson.Compact(payloadDict(p))
	if err != nil {
		return nil, err
	}
	if len(encoded) > maxJSONBytes {
		return nil, apperr.Validation(fmt.Sprintf("配方 JSON 不能超过 %d 字节", maxJSONBytes))
	}
	return encoded, nil
}

func payloadHash(p Payload) (string, error) {
	encoded, err := payloadJSON(p)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:]), nil
}

func jsonHash(payload map[string]any) (string, error) {
	return canonjson.SHA256Hex(payload)
}

func decodeStrict(raw []byte, dest any) error {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dest); err != nil {
		return err
	}
	if dec.More() {
		return fmt.Errorf("trailing")
	}
	return nil
}

func parsePayload(raw []byte) (Payload, error) {
	var wire payloadWire
	if err := decodeStrict(raw, &wire); err != nil {
		return Payload{}, apperr.Conflict("工作流配方内容无效")
	}
	p := payloadFromWire(wire)
	if err := validatePayload(p); err != nil {
		return Payload{}, apperr.Conflict("工作流配方内容无效")
	}
	return p, nil
}

func parsePayloadOrRaise(raw []byte, storedHash string) (Payload, error) {
	p, err := parsePayload(raw)
	if err != nil {
		return Payload{}, err
	}
	got, err := payloadHash(p)
	if err != nil {
		return Payload{}, apperr.Conflict("工作流配方内容无效")
	}
	if got != storedHash {
		return Payload{}, apperr.Conflict("工作流配方 payload hash 不一致")
	}
	return p, nil
}

func payloadFromWire(wire payloadWire) Payload {
	p := Payload{SchemaVersion: wire.SchemaVersion}
	p.Nodes = make([]PayloadNode, 0, len(wire.Nodes))
	for _, node := range wire.Nodes {
		config := node.Config
		if config == nil {
			config = map[string]any{}
		}
		p.Nodes = append(p.Nodes, PayloadNode{
			Key:       node.Key,
			NodeType:  node.NodeType,
			Title:     node.Title,
			PositionX: node.PositionX,
			PositionY: node.PositionY,
			GroupKey:  node.GroupKey,
			Config:    config,
		})
	}
	p.Edges = make([]PayloadEdge, 0, len(wire.Edges))
	for _, edge := range wire.Edges {
		p.Edges = append(p.Edges, PayloadEdge{
			Key:           edge.Key,
			SourceNodeKey: edge.SourceNodeKey,
			TargetNodeKey: edge.TargetNodeKey,
			DataType:      edge.DataType,
			Role:          edge.Role,
			Order:         edge.Order,
		})
	}
	p.Groups = make([]PayloadGroup, 0, len(wire.Groups))
	for _, group := range wire.Groups {
		members := group.MemberKeys
		if members == nil {
			members = []string{}
		}
		p.Groups = append(p.Groups, PayloadGroup{Key: group.Key, Title: group.Title, MemberKeys: members})
	}
	return p
}

func validatePayload(p Payload) error {
	if p.SchemaVersion != schemaVersion {
		return apperr.Validation("工作流配方内容无效")
	}
	if len(p.Nodes) < 1 {
		return apperr.Validation("配方至少需要一个节点")
	}
	if len(p.Nodes) > maxNodes || len(p.Edges) > maxEdges || len(p.Groups) > maxGroups {
		return apperr.Validation("工作流配方内容无效")
	}
	nodeKeys, err := uniqueKeys(func() []string {
		keys := make([]string, 0, len(p.Nodes))
		for _, node := range p.Nodes {
			keys = append(keys, node.Key)
		}
		return keys
	}(), "配方节点 key")
	if err != nil {
		return err
	}
	if _, err := uniqueKeys(func() []string {
		keys := make([]string, 0, len(p.Edges))
		for _, edge := range p.Edges {
			keys = append(keys, edge.Key)
		}
		return keys
	}(), "配方连线 key"); err != nil {
		return err
	}
	groupKeys, err := uniqueKeys(func() []string {
		keys := make([]string, 0, len(p.Groups))
		for _, group := range p.Groups {
			keys = append(keys, group.Key)
		}
		return keys
	}(), "配方分组 key")
	if err != nil {
		return err
	}
	memberGroupKeys := map[string]struct{}{}
	for i := range p.Nodes {
		node := &p.Nodes[i]
		key, err := normalizeRecipeKey(node.Key)
		if err != nil {
			return err
		}
		node.Key = key
		title, err := normalizeRecipeText(node.Title)
		if err != nil {
			return err
		}
		node.Title = title
		if _, ok := validNodeTypes[node.NodeType]; !ok {
			return apperr.Validation("工作流配方内容无效")
		}
		if node.GroupKey != nil {
			gk, err := normalizeRecipeKey(*node.GroupKey)
			if err != nil {
				return err
			}
			if _, ok := groupKeys[gk]; !ok {
				return apperr.Validation("配方节点引用了不存在的分组")
			}
			node.GroupKey = strPtr(gk)
			memberGroupKeys[gk] = struct{}{}
		}
		if err := validateNodeConfig(node.NodeType, node.Config); err != nil {
			return err
		}
	}
	nodeKeys, err = uniqueKeys(func() []string {
		keys := make([]string, 0, len(p.Nodes))
		for _, node := range p.Nodes {
			keys = append(keys, node.Key)
		}
		return keys
	}(), "配方节点 key")
	if err != nil {
		return err
	}
	if len(memberGroupKeys) != len(groupKeys) {
		return apperr.Validation("配方分组必须至少包含一个节点")
	}
	for i := range p.Groups {
		group := &p.Groups[i]
		key, err := normalizeRecipeKey(group.Key)
		if err != nil {
			return err
		}
		group.Key = key
		title, err := normalizeRecipeText(group.Title)
		if err != nil {
			return err
		}
		group.Title = title
		if len(group.MemberKeys) < 1 {
			return apperr.Validation("配方分组必须至少包含一个节点")
		}
		normalizedMembers := make([]string, 0, len(group.MemberKeys))
		for _, member := range group.MemberKeys {
			mk, err := normalizeRecipeKey(member)
			if err != nil {
				return err
			}
			normalizedMembers = append(normalizedMembers, mk)
		}
		if _, err := uniqueKeys(normalizedMembers, "配方分组成员"); err != nil {
			return err
		}
		group.MemberKeys = normalizedMembers
		for _, member := range group.MemberKeys {
			if _, ok := nodeKeys[member]; !ok {
				return apperr.Validation("配方分组引用了不存在的节点")
			}
		}
		expected := map[string]struct{}{}
		for _, node := range p.Nodes {
			if node.GroupKey != nil && *node.GroupKey == group.Key {
				expected[node.Key] = struct{}{}
			}
		}
		if len(expected) != len(group.MemberKeys) {
			return apperr.Validation("配方分组成员必须与节点 group_key 一致")
		}
		for _, member := range group.MemberKeys {
			if _, ok := expected[member]; !ok {
				return apperr.Validation("配方分组成员必须与节点 group_key 一致")
			}
		}
	}
	pairs := map[[2]string]struct{}{}
	for i := range p.Edges {
		edge := &p.Edges[i]
		key, err := normalizeRecipeKey(edge.Key)
		if err != nil {
			return err
		}
		edge.Key = key
		src, err := normalizeRecipeKey(edge.SourceNodeKey)
		if err != nil {
			return err
		}
		dst, err := normalizeRecipeKey(edge.TargetNodeKey)
		if err != nil {
			return err
		}
		edge.SourceNodeKey = src
		edge.TargetNodeKey = dst
		if _, ok := validDataTypes[edge.DataType]; !ok {
			return apperr.Validation("工作流配方内容无效")
		}
		if _, ok := validRoles[edge.Role]; !ok {
			return apperr.Validation("工作流配方内容无效")
		}
		if edge.Order < 0 {
			return apperr.Validation("工作流配方内容无效")
		}
		if _, ok := nodeKeys[src]; !ok {
			return apperr.Validation("配方连线引用了不存在的节点")
		}
		if _, ok := nodeKeys[dst]; !ok {
			return apperr.Validation("配方连线引用了不存在的节点")
		}
		if src == dst {
			return apperr.Validation("配方连线不能连接节点自身")
		}
		pair := [2]string{src, dst}
		if _, ok := pairs[pair]; ok {
			return apperr.Validation("配方节点之间不能重复连线")
		}
		pairs[pair] = struct{}{}
	}
	return nil
}

func validateNodeConfig(nodeType graph.NodeType, config map[string]any) error {
	if config == nil {
		config = map[string]any{}
	}
	var illegal []string
	for key := range config {
		if _, ok := forbiddenPlanKeys[key]; ok {
			illegal = append(illegal, key)
		}
	}
	if len(illegal) > 0 {
		sort.Strings(illegal)
		return apperr.Validation("配方节点配置不能包含拓扑字段: " + strings.Join(illegal, ", "))
	}
	if _, err := graph.NormalizeNodeConfig(nodeType, config); err != nil {
		return err
	}
	return nil
}

func parseGovernance(raw []byte) ([]string, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return []string{}, nil
	}
	var wire governanceWire
	if err := decodeStrict(raw, &wire); err != nil {
		return nil, apperr.Conflict("工作流配方治理元数据无效")
	}
	if err := uniqueStringList(wire.ApplicableImageTypes, "适用图片类型"); err != nil {
		return nil, apperr.Conflict("工作流配方治理元数据无效")
	}
	if err := uniqueStringList(wire.RequiredInputs, "配方所需输入"); err != nil {
		return nil, apperr.Conflict("工作流配方治理元数据无效")
	}
	if len(wire.ApplicableImageTypes) < 1 || len(wire.ApplicableImageTypes) > 32 {
		return nil, apperr.Conflict("工作流配方治理元数据无效")
	}
	if len(wire.RequiredInputs) > 32 {
		return nil, apperr.Conflict("工作流配方治理元数据无效")
	}
	if _, err := normalizeRecipeText(wire.DefaultResult); err != nil {
		return nil, apperr.Conflict("工作流配方治理元数据无效")
	}
	if err := optionalRef(wire.Thumbnail); err != nil {
		return nil, apperr.Conflict("工作流配方治理元数据无效")
	}
	if err := optionalRef(wire.ProviderSample); err != nil {
		return nil, apperr.Conflict("工作流配方治理元数据无效")
	}
	if wire.RequiredInputs == nil {
		return []string{}, nil
	}
	return append([]string{}, wire.RequiredInputs...), nil
}

func optionalRef(v *string) error {
	if v == nil {
		return nil
	}
	s := strings.TrimSpace(*v)
	if s == "" || len(s) > maxRefLen {
		return apperr.Validation("invalid")
	}
	return nil
}

func uniqueStringList(values []string, label string) error {
	_, err := uniqueKeys(values, label)
	return err
}

func uniqueKeys(values []string, label string) (map[string]struct{}, error) {
	out := map[string]struct{}{}
	for _, value := range values {
		if _, ok := out[value]; ok {
			return nil, apperr.Validation(label + " 不能重复")
		}
		out[value] = struct{}{}
	}
	return out, nil
}

func normalizeRecipeKey(raw string) (string, error) {
	key := strings.TrimSpace(raw)
	if key == "" || len(key) > maxKeyLen || !recipeKeyRE.MatchString(key) {
		return "", apperr.Validation("工作流配方内容无效")
	}
	return key, nil
}

func normalizeRecipeText(raw string) (string, error) {
	title := strings.TrimSpace(raw)
	if title == "" || len([]rune(title)) > maxTitleLen {
		return "", apperr.Validation("工作流配方内容无效")
	}
	return title, nil
}

func strPtr(v string) *string { return &v }

func cloneValue(value any) any {
	switch t := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(t))
		for key, item := range t {
			out[key] = cloneValue(item)
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i, item := range t {
			out[i] = cloneValue(item)
		}
		return out
	case []string:
		out := make([]string, len(t))
		copy(out, t)
		return out
	default:
		return value
	}
}

func limitRunes(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:n])
}
