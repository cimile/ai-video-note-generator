package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/peterbourgon/ff/v3/ffcli"
	"github.com/sashabaranov/go-openai"
)

type Config struct {
	OpenAIAPIKey string `json:"openai_api_key"`
	Model        string `json:"model"`
}

func main() {
	config := &Config{}
	configFile := flag.String("config", "config.json", "配置文件路径")
	flag.Parse()

	if err := loadConfig(*configFile, config); err != nil {
		log.Fatalf("加载配置文件失败: %v", err)
	}

	if config.OpenAIAPIKey == "" {
		log.Fatal("OpenAI API Key 不能为空")
	}

	root := &ffcli.Command{
		Name:       "video-note",
		ShortUsage: "video-note [flags] <subcommand>",
		Subcommands: []*ffcli.Command{
			generateCommand(config),
			transcribeCommand(config),
			summarizeCommand(config),
		},
	}

	if err := root.ParseAndRun(context.Background(), os.Args[1:]); err != nil {
		log.Fatal(err)
	}
}

func loadConfig(path string, config *Config) error {
	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("打开配置文件失败: %w", err)
	}
	defer file.Close()

	bytes, err := io.ReadAll(file)
	if err != nil {
		return fmt.Errorf("读取配置文件失败: %w", err)
	}

	if err := json.Unmarshal(bytes, config); err != nil {
		return fmt.Errorf("解析配置文件失败: %w", err)
	}

	return nil
}

func generateCommand(config *Config) *ffcli.Command {
	var (
		videoPath    string
		outputPath   string
		summaryRatio float64
	)

	cmd := &ffcli.Command{
		Name:       "generate",
		ShortUsage: "video-note generate [flags] -i video.mp4 -o notes.txt",
		ShortHelp:  "从视频生成笔记",
		FlagSet:    flag.NewFlagSet("video-note generate", flag.ExitOnError),
		Exec: func(ctx context.Context, args []string) error {
			if videoPath == "" {
				return fmt.Errorf("必须指定视频文件 (-i)")
			}

			if outputPath == "" {
				ext := filepath.Ext(videoPath)
				outputPath = strings.TrimSuffix(videoPath, ext) + ".txt"
			}

			// 临时文件
			tmpDir, err := os.MkdirTemp("", "video-note-")
			if err != nil {
				return fmt.Errorf("创建临时目录失败: %w", err)
			}
			defer os.RemoveAll(tmpDir)

			audioPath := filepath.Join(tmpDir, "audio.mp3")
			transcriptPath := filepath.Join(tmpDir, "transcript.txt")

			// 1. 提取音频
			log.Printf("正在从视频中提取音频...")
			if err := extractAudio(videoPath, audioPath); err != nil {
				return fmt.Errorf("提取音频失败: %w", err)
			}

			// 2. 音频转文字
			log.Printf("正在将音频转换为文字...")
			if err := transcribeAudio(ctx, config.OpenAIAPIKey, config.Model, audioPath, transcriptPath); err != nil {
				return fmt.Errorf("音频转文字失败: %w", err)
			}

			// 3. 生成摘要
			log.Printf("正在生成笔记摘要...")
			if err := summarizeText(ctx, config.OpenAIAPIKey, config.Model, transcriptPath, outputPath, summaryRatio); err != nil {
				return fmt.Errorf("生成摘要失败: %w", err)
			}

			log.Printf("笔记已生成: %s", outputPath)
			return nil
		},
	}

	cmd.FlagSet.StringVar(&videoPath, "i", "", "输入视频文件路径")
	cmd.FlagSet.StringVar(&outputPath, "o", "", "输出笔记文件路径 (默认与视频同名)")
	cmd.FlagSet.Float64Var(&summaryRatio, "ratio", 0.2, "摘要比例 (0.1-0.5)")

	return cmd
}

func extractAudio(videoPath, audioPath string) error {
	cmd := exec.Command("ffmpeg", "-i", videoPath, "-vn", "-acodec", "libmp3lame", audioPath)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("ffmpeg执行失败: %w\n输出: %s", err, string(output))
	}
	return nil
}

func transcribeAudio(ctx context.Context, apiKey, model, audioPath, outputPath string) error {
	client := openai.NewClient(apiKey)

	file, err := os.Open(audioPath)
	if err != nil {
		return fmt.Errorf("打开音频文件失败: %w", err)
	}
	defer file.Close()

	req := openai.AudioTranscriptionRequest{
		Model:    model,
		FilePath: audioPath,
	}

	transcript, err := client.CreateTranscription(ctx, req)
	if err != nil {
		return fmt.Errorf("调用OpenAI API失败: %w", err)
	}

	outputFile, err := os.Create(outputPath)
	if err != nil {
		return fmt.Errorf("创建输出文件失败: %w", err)
	}
	defer outputFile.Close()

	if _, err := outputFile.WriteString(transcript.Text); err != nil {
		return fmt.Errorf("写入转录文本失败: %w", err)
	}

	return nil
}

func summarizeText(ctx context.Context, apiKey, model, inputPath, outputPath string, ratio float64) error {
	// 限制摘要比例范围
	if ratio < 0.1 {
		ratio = 0.1
	} else if ratio > 0.5 {
		ratio = 0.5
	}

	client := openai.NewClient(apiKey)

	// 读取转录文本
	transcript, err := os.ReadFile(inputPath)
	if err != nil {
		return fmt.Errorf("读取转录文件失败: %w", err)
	}

	// 分割文本为多个块，避免超出token限制
	chunks := splitTextIntoChunks(string(transcript), 3000)
	var summaries []string
	var wg sync.WaitGroup
	errChan := make(chan error, len(chunks))

	for i, chunk := range chunks {
		wg.Add(1)
		go func(idx int, text string) {
			defer wg.Done()

			// 等待一段时间，避免API请求过于频繁
			time.Sleep(time.Duration(idx*2) * time.Second)

			prompt := fmt.Sprintf(`请为以下视频转录内容生成详细的笔记摘要，保留关键信息和重要细节:
			
内容:
%s

请生成一份简洁但信息丰富的摘要，约占原文长度的%.0f%%。`, text, ratio*100)

			resp, err := client.CreateChatCompletion(ctx, openai.ChatCompletionRequest{
				Model: model,
				Messages: []openai.ChatCompletionMessage{
					{
						Role:    openai.ChatMessageRoleUser,
						Content: prompt,
					},
				},
				Temperature: 0.3,
				MaxTokens:   int(float64(len(text)) * ratio * 1.5),
			})

			if err != nil {
				errChan <- fmt.Errorf("生成第%d部分摘要失败: %w", idx+1, err)
				return
			}

			summaries = append(summaries, resp.Choices[0].Message.Content)
		}(i, chunk)
	}

	go func() {
		wg.Wait()
		close(errChan)
	}()

	for err := range errChan {
		if err != nil {
			return err
		}
	}

	// 合并所有摘要部分
	combinedSummary := strings.Join(summaries, "\n\n--- 第%d部分结束 ---\n\n")

	// 写入输出文件
	if err := os.WriteFile(outputPath, []byte(combinedSummary), 0644); err != nil {
		return fmt.Errorf("写入摘要文件失败: %w", err)
	}

	return nil
}

func splitTextIntoChunks(text string, chunkSize int) []string {
	var chunks []string
	words := strings.Fields(text)
	currentChunk := ""

	for _, word := range words {
		if len(currentChunk)+len(word)+1 > chunkSize {
			chunks = append(chunks, currentChunk)
			currentChunk = word
		} else {
			if currentChunk == "" {
				currentChunk = word
			} else {
				currentChunk += " " + word
			}
		}
	}

	if currentChunk != "" {
		chunks = append(chunks, currentChunk)
	}

	return chunks
}

func transcribeCommand(config *Config) *ffcli.Command {
	var (
		audioPath  string
		outputPath string
	)

	cmd := &ffcli.Command{
		Name:       "transcribe",
		ShortUsage: "video-note transcribe [flags] -i audio.mp3 -o transcript.txt",
		ShortHelp:  "将音频文件转换为文字",
		FlagSet:    flag.NewFlagSet("video-note transcribe", flag.ExitOnError),
		Exec: func(ctx context.Context, args []string) error {
			if audioPath == "" {
				return fmt.Errorf("必须指定音频文件 (-i)")
			}

			if outputPath == "" {
				ext := filepath.Ext(audioPath)
				outputPath = strings.TrimSuffix(audioPath, ext) + ".txt"
			}

			log.Printf("正在将音频转换为文字...")
			if err := transcribeAudio(ctx, config.OpenAIAPIKey, config.Model, audioPath, outputPath); err != nil {
				return fmt.Errorf("音频转文字失败: %w", err)
			}

			log.Printf("转录完成: %s", outputPath)
			return nil
		},
	}

	cmd.FlagSet.StringVar(&audioPath, "i", "", "输入音频文件路径")
	cmd.FlagSet.StringVar(&outputPath, "o", "", "输出转录文件路径 (默认与音频同名)")

	return cmd
}

func summarizeCommand(config *Config) *ffcli.Command {
	var (
		inputPath    string
		outputPath   string
		summaryRatio float64
	)

	cmd := &ffcli.Command{
		Name:       "summarize",
		ShortUsage: "video-note summarize [flags] -i transcript.txt -o summary.txt",
		ShortHelp:  "从文本生成摘要笔记",
		FlagSet:    flag.NewFlagSet("video-note summarize", flag.ExitOnError),
		Exec: func(ctx context.Context, args []string) error {
			if inputPath == "" {
				return fmt.Errorf("必须指定输入文本文件 (-i)")
			}

			if outputPath == "" {
				ext := filepath.Ext(inputPath)
				outputPath = strings.TrimSuffix(inputPath, ext) + ".summary.txt"
			}

			if summaryRatio < 0.1 || summaryRatio > 0.5 {
				return fmt.Errorf("摘要比例必须在0.1-0.5之间")
			}

			log.Printf("正在生成笔记摘要...")
			if err := summarizeText(ctx, config.OpenAIAPIKey, config.Model, inputPath, outputPath, summaryRatio); err != nil {
				return fmt.Errorf("生成摘要失败: %w", err)
			}

			log.Printf("摘要已生成: %s", outputPath)
			return nil
		},
	}

	cmd.FlagSet.StringVar(&inputPath, "i", "", "输入文本文件路径")
	cmd.FlagSet.StringVar(&outputPath, "o", "", "输出摘要文件路径 (默认与输入同名)")
	cmd.FlagSet.Float64Var(&summaryRatio, "ratio", 0.2, "摘要比例 (0.1-0.5)")

	return cmd
}    