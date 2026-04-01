package main

import (
        "context"
        "fmt"
        "math"
        "strings"
        "time"

        "github.com/jig/lisp"
        "github.com/jig/lisp/env"
        "github.com/jig/lisp/lib/concurrent/nsconcurrent"
        "github.com/jig/lisp/lib/core/nscore"
        "github.com/jig/lisp/lib/coreextented/nscoreextended"
        "github.com/jig/lisp/types"
)

// schemeEval 执行 Clojure/Lisp 表达式并返回结果字符串
// 使用 jig/lisp 库（纯 Go 实现，Clojure 方言）
func schemeEval(ctx context.Context, expr string) (string, error) {
        expr = strings.TrimSpace(expr)
        if expr == "" {
                return "", fmt.Errorf("expression is empty")
        }

        // 创建 Lisp 环境并加载核心函数
        e := env.NewEnv()
        if err := nscore.Load(e); err != nil {
                return "", fmt.Errorf("failed to load core: %v", err)
        }

        // 加载并发原语 (atom, swap!, reset! 等，coreextented 依赖)
        if err := nsconcurrent.Load(e); err != nil {
                return "", fmt.Errorf("failed to load concurrent: %v", err)
        }

        // 加载扩展库 (reduce, inc, dec, zero?, identity, gensym 等)
        if err := nscoreextended.Load(e); err != nil {
                return "", fmt.Errorf("failed to load core-ext: %v", err)
        }

        // 注册扩展数学函数和常量（覆盖核心 +, -, *, / 为可变参数版本）
        registerMathFuncs(e)

        // 加载常用 Lisp 函数扩展（filter, range, even?, odd? 等）
        if _, err := lisp.REPL(context.Background(), e, lispPreamble(), nil); err != nil {
                return "", fmt.Errorf("failed to load preamble: %v", err)
        }

        // 始终包装在 (do ...) 中以支持多表达式输入
        // REPL 的 READ 只读一个 form，do 块可顺序执行多个 form 并返回最后一个结果
        wrappedExpr := "(do\n" + expr + "\n)"

        // 5 秒超时
        evalCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
        defer cancel()

        startTime := time.Now()
        result, err := lisp.REPL(evalCtx, e, wrappedExpr, nil)
        elapsed := time.Since(startTime)

        if err != nil {
                return "", err
        }

        // REPL 内部调用 PRINT 返回 string 类型的 MalType
        resultStr, _ := result.(string)
        output := fmt.Sprintf("%s\n\n[耗时 %v]", resultStr, elapsed.Round(time.Millisecond))
        return output, nil
}

// lispPreamble 返回常用 Lisp 扩展函数定义
func lispPreamble() string {
        return `(do
  ;; 谓词函数
  (defn even? [n] (= 0 (mod n 2)))
  (defn odd? [n] (not (= 0 (mod n 2))))
  (defn pos? [n] (> n 0))
  (defn neg? [n] (< n 0))
  (defn zero? [n] (= 0 n))

  ;; 列表函数
  ;; 注意: jig/lisp 的 cons 不接受 nil 作为第二个参数，需使用 '() 代替
  (defn filter [pred xs]
    (if (empty? xs) '()
      (if (pred (first xs))
        (cons (first xs) (filter pred (rest xs)))
        (filter pred (rest xs)))))

  (defn range [n]
    (if (<= n 0) '()
      (let [r (range (dec n))]
        (cons (dec n) (if (empty? r) '() r)))))

  (defn reverse [xs]
    (defn rev-helper [xs acc]
      (if (empty? xs) acc
        (rev-helper (rest xs) (cons (first xs) (if (empty? acc) '() acc)))))
    (rev-helper xs '()))

  (defn last [xs]
    (if (empty? (rest xs)) (first xs)
      (last (rest xs))))

  (defn butlast [xs]
    (reverse (rest (reverse xs))))

  (defn take-while [pred xs]
    (if (or (empty? xs) (not (pred (first xs)))) '()
      (cons (first xs) (take-while pred (rest xs)))))

  (defn drop-while [pred xs]
    (if (or (empty? xs) (not (pred (first xs)))) xs
      (drop-while pred (rest xs))))

  ;; 求和 / 求积
  (defn sum [xs] (reduce + 0 xs))
  (defn product [xs] (reduce * 1 xs))

  ;; 字符串处理
  (defn str-join [sep xs]
    (if (empty? xs) ""
      (if (= 1 (count xs)) (str (first xs))
        (str (first xs) sep (str-join sep (rest xs))))))

  ;; 应用函数到列表
  (defn apply-fn [f args] (apply f args))
)`
}

// registerMathFuncs 注册扩展数学常量和函数到 Lisp 环境
// 覆盖核心的二元 +, -, *, / 为可变参数版本，并补充浮点运算和数学函数
func registerMathFuncs(e types.EnvType) {
        // ---- 数学常量 ----
        _ = e.Set(types.Symbol{Val: "PI"}, 3.141592653589793)
        _ = e.Set(types.Symbol{Val: "E"}, 2.718281828459045)

        // ---- 可变参数整数算术（覆盖核心二元版本）----
        _ = e.Set(types.Symbol{Val: "+"}, types.Func{Fn: func(_ context.Context, args []types.MalType) (types.MalType, error) {
                result := 0
                for _, a := range args {
                        result += toInt(a)
                }
                return result, nil
        }})
        _ = e.Set(types.Symbol{Val: "-"}, types.Func{Fn: func(_ context.Context, args []types.MalType) (types.MalType, error) {
                if len(args) == 0 {
                        return 0, nil
                }
                result := toInt(args[0])
                for _, a := range args[1:] {
                        result -= toInt(a)
                }
                return result, nil
        }})
        _ = e.Set(types.Symbol{Val: "*"}, types.Func{Fn: func(_ context.Context, args []types.MalType) (types.MalType, error) {
                result := 1
                for _, a := range args {
                        result *= toInt(a)
                }
                return result, nil
        }})
        _ = e.Set(types.Symbol{Val: "/"}, types.Func{Fn: func(_ context.Context, args []types.MalType) (types.MalType, error) {
                if len(args) == 0 {
                        return 0, nil
                }
                result := toInt(args[0])
                for _, a := range args[1:] {
                        d := toInt(a)
                        if d == 0 {
                                return nil, fmt.Errorf("division by zero")
                        }
                        result /= d
                }
                return result, nil
        }})

        // ---- 浮点算术（f+ f- f* f/）----
        _ = e.Set(types.Symbol{Val: "f+"}, types.Func{Fn: func(_ context.Context, args []types.MalType) (types.MalType, error) {
                result := 0.0
                for _, a := range args {
                        result += toFloat64(a)
                }
                return result, nil
        }})
        _ = e.Set(types.Symbol{Val: "f-"}, types.Func{Fn: func(_ context.Context, args []types.MalType) (types.MalType, error) {
                if len(args) == 0 {
                        return 0.0, nil
                }
                result := toFloat64(args[0])
                for _, a := range args[1:] {
                        result -= toFloat64(a)
                }
                return result, nil
        }})
        _ = e.Set(types.Symbol{Val: "f*"}, types.Func{Fn: func(_ context.Context, args []types.MalType) (types.MalType, error) {
                result := 1.0
                for _, a := range args {
                        result *= toFloat64(a)
                }
                return result, nil
        }})
        _ = e.Set(types.Symbol{Val: "f/"}, types.Func{Fn: func(_ context.Context, args []types.MalType) (types.MalType, error) {
                if len(args) == 0 {
                        return 0.0, nil
                }
                result := toFloat64(args[0])
                for _, a := range args[1:] {
                        d := toFloat64(a)
                        if d == 0 {
                                return nil, fmt.Errorf("division by zero")
                        }
                        result /= d
                }
                return result, nil
        }})

        // ---- 单参数数学函数 ----
        _ = e.Set(types.Symbol{Val: "sqrt"}, types.Func{Fn: makeMathFunc1(math.Sqrt)})
        _ = e.Set(types.Symbol{Val: "abs"}, types.Func{Fn: makeMathFunc1(math.Abs)})
        _ = e.Set(types.Symbol{Val: "floor"}, types.Func{Fn: makeMathFunc1(math.Floor)})
        _ = e.Set(types.Symbol{Val: "ceil"}, types.Func{Fn: makeMathFunc1(math.Ceil)})
        _ = e.Set(types.Symbol{Val: "round"}, types.Func{Fn: makeMathFunc1(math.Round)})
        _ = e.Set(types.Symbol{Val: "log"}, types.Func{Fn: makeMathFunc1(math.Log)})
        _ = e.Set(types.Symbol{Val: "log10"}, types.Func{Fn: makeMathFunc1(math.Log10)})
        _ = e.Set(types.Symbol{Val: "log2"}, types.Func{Fn: makeMathFunc1(math.Log2)})
        _ = e.Set(types.Symbol{Val: "exp"}, types.Func{Fn: makeMathFunc1(math.Exp)})
        _ = e.Set(types.Symbol{Val: "sin"}, types.Func{Fn: makeMathFunc1(math.Sin)})
        _ = e.Set(types.Symbol{Val: "cos"}, types.Func{Fn: makeMathFunc1(math.Cos)})
        _ = e.Set(types.Symbol{Val: "tan"}, types.Func{Fn: makeMathFunc1(math.Tan)})
        _ = e.Set(types.Symbol{Val: "asin"}, types.Func{Fn: makeMathFunc1(math.Asin)})
        _ = e.Set(types.Symbol{Val: "acos"}, types.Func{Fn: makeMathFunc1(math.Acos)})
        _ = e.Set(types.Symbol{Val: "atan"}, types.Func{Fn: makeMathFunc1(math.Atan)})

        // ---- 双参数数学函数 ----
        _ = e.Set(types.Symbol{Val: "pow"}, types.Func{Fn: makeMathFunc2(math.Pow)})
        _ = e.Set(types.Symbol{Val: "atan2"}, types.Func{Fn: makeMathFunc2(math.Atan2)})
        _ = e.Set(types.Symbol{Val: "min"}, types.Func{Fn: makeMathFunc2(math.Min)})
        _ = e.Set(types.Symbol{Val: "max"}, types.Func{Fn: makeMathFunc2(math.Max)})
        _ = e.Set(types.Symbol{Val: "mod"}, types.Func{Fn: makeMathFunc2(math.Mod)})
        _ = e.Set(types.Symbol{Val: "remainder"}, types.Func{Fn: makeMathFunc2(math.Remainder)})
}

// makeMathFunc1 创建单参数数学函数的 types.Func 包装（float64 输入输出）
func makeMathFunc1(fn func(float64) float64) types.ExternalCall {
        return func(_ context.Context, args []types.MalType) (types.MalType, error) {
                if len(args) != 1 {
                        return nil, fmt.Errorf("expected 1 argument, got %d", len(args))
                }
                return fn(toFloat64(args[0])), nil
        }
}

// makeMathFunc2 创建双参数数学函数的 types.Func 包装（float64 输入输出）
func makeMathFunc2(fn func(float64, float64) float64) types.ExternalCall {
        return func(_ context.Context, args []types.MalType) (types.MalType, error) {
                if len(args) != 2 {
                        return nil, fmt.Errorf("expected 2 arguments, got %d", len(args))
                }
                return fn(toFloat64(args[0]), toFloat64(args[1])), nil
        }
}

// toFloat64 将 MalType 转换为 float64
// 支持 int, float64, float32（jig/lisp reader 将浮点字面量解析为 float32）
func toFloat64(v types.MalType) float64 {
        switch v := v.(type) {
        case int:
                return float64(v)
        case float64:
                return v
        case float32:
                return float64(v)
        case bool:
                if v {
                        return 1.0
                }
                return 0.0
        default:
                return 0.0
        }
}

// toInt 将 MalType 转换为 int
func toInt(v types.MalType) int {
        switch v := v.(type) {
        case int:
                return v
        case float64:
                return int(v)
        case float32:
                return int(v)
        case bool:
                if v {
                        return 1
                }
                return 0
        default:
                return 0
        }
}
