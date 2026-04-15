# API参考

## 技能系统API

### 加载技能
```go
skill, err := skillManager.GetSkill("test_skill")
```

### 构建技能提示
```go
prompt := skill.BuildSkillPrompt()
```