// Package skills discovers Agent Skills and exposes their metadata without
// eagerly loading full instructions into every model request.
package skills

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"

	"go.yaml.in/yaml/v3"
)

const maxSkillFileBytes = 256 << 10

var validSkillName = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)

type Scope string

const (
	ScopeRepository Scope = "repository"
	ScopeUser       Scope = "user"
)

// Skill is the metadata initially exposed to the model and TUI.
type Skill struct {
	Name        string
	Description string
	Path        string
	Scope       Scope
}

type catalogEntry struct {
	Skill
	canonicalRoot string
	canonicalFile string
}

// Catalog is an immutable startup snapshot. Existing files are revalidated
// when loaded, so content edits do not require rediscovery.
type Catalog struct {
	skills   []Skill
	byPath   map[string]catalogEntry
	warnings []string
}

type Options struct {
	WorkingDir string
	UserHome   string
}

type scanRoot struct {
	path  string
	scope Scope
}

// Discover scans .agents/skills from the working directory to the repository
// root, followed by ~/.agents/skills and the Codex-compatible user directory.
func Discover(options Options) (*Catalog, error) {
	workingDir, err := canonicalDirectory(options.WorkingDir)
	if err != nil {
		return nil, fmt.Errorf("定位 Skill 工作区: %w", err)
	}
	roots := repositorySkillRoots(workingDir)
	userHome := strings.TrimSpace(options.UserHome)
	if userHome == "" {
		userHome, err = os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("定位用户 Skill 目录: %w", err)
		}
	}
	if userHome != "" {
		absoluteHome, err := filepath.Abs(userHome)
		if err != nil {
			return nil, fmt.Errorf("定位用户 Skill 目录: %w", err)
		}
		roots = append(roots, scanRoot{path: filepath.Join(absoluteHome, ".agents", "skills"), scope: ScopeUser})
		codexHome := ""
		if strings.TrimSpace(options.UserHome) == "" {
			codexHome = strings.TrimSpace(os.Getenv("CODEX_HOME"))
		}
		if codexHome == "" {
			codexHome = filepath.Join(absoluteHome, ".codex")
		} else if !filepath.IsAbs(codexHome) {
			codexHome = filepath.Join(absoluteHome, codexHome)
		}
		roots = append(roots, scanRoot{path: filepath.Join(codexHome, "skills"), scope: ScopeUser})
	}

	catalog := &Catalog{byPath: make(map[string]catalogEntry)}
	for _, root := range roots {
		catalog.scan(root)
	}
	return catalog, nil
}

func (c *Catalog) Skills() []Skill {
	if c == nil {
		return nil
	}
	return append([]Skill(nil), c.skills...)
}

func (c *Catalog) Warnings() []string {
	if c == nil {
		return nil
	}
	return append([]string(nil), c.warnings...)
}

func (c *Catalog) scan(root scanRoot) {
	entries, err := os.ReadDir(root.path)
	if errors.Is(err, os.ErrNotExist) {
		return
	}
	if err != nil {
		c.warn(root.path, err)
		return
	}
	for _, directory := range entries {
		skillDir := filepath.Join(root.path, directory.Name())
		info, err := os.Stat(skillDir)
		if err != nil {
			c.warn(skillDir, err)
			continue
		}
		if !info.IsDir() {
			continue
		}
		path := filepath.Join(skillDir, "SKILL.md")
		content, err := readTextFile(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			c.warn(path, err)
			continue
		}
		metadata, err := parseMetadata(content, directory.Name())
		if err != nil {
			c.warn(path, err)
			continue
		}
		logicalPath, err := filepath.Abs(path)
		if err != nil {
			c.warn(path, err)
			continue
		}
		logicalPath = filepath.Clean(logicalPath)
		if _, duplicate := c.byPath[logicalPath]; duplicate {
			continue
		}
		canonicalRoot, err := filepath.EvalSymlinks(skillDir)
		if err != nil {
			c.warn(skillDir, err)
			continue
		}
		canonicalFile, err := filepath.EvalSymlinks(path)
		if err != nil || !within(canonicalRoot, canonicalFile) {
			if err == nil {
				err = errors.New("SKILL.md 指向 Skill 目录外")
			}
			c.warn(path, err)
			continue
		}
		skill := Skill{Name: metadata.Name, Description: metadata.Description, Path: logicalPath, Scope: root.scope}
		c.skills = append(c.skills, skill)
		c.byPath[logicalPath] = catalogEntry{Skill: skill, canonicalRoot: canonicalRoot, canonicalFile: canonicalFile}
	}
}

func (c *Catalog) warn(path string, err error) {
	c.warnings = append(c.warnings, fmt.Sprintf("%s: %v", path, err))
}

func repositorySkillRoots(workingDir string) []scanRoot {
	repositoryRoot := findRepositoryRoot(workingDir)
	current := workingDir
	roots := make([]scanRoot, 0, 4)
	for {
		roots = append(roots, scanRoot{path: filepath.Join(current, ".agents", "skills"), scope: ScopeRepository})
		if current == repositoryRoot {
			break
		}
		parent := filepath.Dir(current)
		if parent == current {
			break
		}
		current = parent
	}
	return roots
}

func findRepositoryRoot(workingDir string) string {
	current := workingDir
	for {
		if _, err := os.Stat(filepath.Join(current, ".git")); err == nil {
			return current
		}
		parent := filepath.Dir(current)
		if parent == current {
			return workingDir
		}
		current = parent
	}
}

func canonicalDirectory(path string) (string, error) {
	if strings.TrimSpace(path) == "" {
		path = "."
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", errors.New("不是目录")
	}
	return resolved, nil
}

type frontmatter struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
}

func parseMetadata(content []byte, directoryName string) (frontmatter, error) {
	header, err := frontmatterBytes(content)
	if err != nil {
		return frontmatter{}, err
	}
	var metadata frontmatter
	if err := yaml.Unmarshal(header, &metadata); err != nil {
		return frontmatter{}, fmt.Errorf("YAML frontmatter 无效: %w", err)
	}
	metadata.Name = strings.TrimSpace(metadata.Name)
	metadata.Description = strings.TrimSpace(metadata.Description)
	if len(metadata.Name) == 0 || len(metadata.Name) > 64 || !validSkillName.MatchString(metadata.Name) {
		return frontmatter{}, errors.New("name 必须是 1-64 位小写字母、数字或单个连字符")
	}
	if metadata.Name != directoryName {
		return frontmatter{}, fmt.Errorf("name %q 与父目录 %q 不一致", metadata.Name, directoryName)
	}
	if metadata.Description == "" || utf8.RuneCountInString(metadata.Description) > 1024 {
		return frontmatter{}, errors.New("description 必须是 1-1024 个字符")
	}
	return metadata, nil
}

func frontmatterBytes(content []byte) ([]byte, error) {
	scanner := bufio.NewScanner(bytes.NewReader(content))
	scanner.Buffer(make([]byte, 4096), maxSkillFileBytes)
	if !scanner.Scan() || strings.TrimSpace(scanner.Text()) != "---" {
		return nil, errors.New("缺少起始 YAML frontmatter")
	}
	var header strings.Builder
	for scanner.Scan() {
		if strings.TrimSpace(scanner.Text()) == "---" {
			return []byte(header.String()), nil
		}
		header.WriteString(scanner.Text())
		header.WriteByte('\n')
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return nil, errors.New("缺少结束 YAML frontmatter")
}

func readTextFile(path string) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("不是普通文件")
	}
	reader := io.LimitReader(file, maxSkillFileBytes+1)
	content, err := io.ReadAll(reader)
	if err != nil {
		return nil, err
	}
	if len(content) > maxSkillFileBytes {
		return nil, fmt.Errorf("超过 %d KiB 上限", maxSkillFileBytes>>10)
	}
	if !utf8.Valid(content) {
		return nil, errors.New("不是 UTF-8 文本")
	}
	return content, nil
}

func within(root, candidate string) bool {
	relative, err := filepath.Rel(root, candidate)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

func sortedSkills(skills []Skill) []Skill {
	copySkills := append([]Skill(nil), skills...)
	sort.SliceStable(copySkills, func(i, j int) bool {
		if copySkills[i].Name == copySkills[j].Name {
			return copySkills[i].Path < copySkills[j].Path
		}
		return copySkills[i].Name < copySkills[j].Name
	})
	return copySkills
}
