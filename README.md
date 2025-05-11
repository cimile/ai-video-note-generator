# AI视频笔记生成工具

这是一个使用Go语言编写的AI视频笔记生成工具，可以自动为视频生成文本笔记。工具通过提取视频中的音频，将音频转换为文字，然后使用AI生成摘要笔记。

## 功能特点
- 从视频中提取音频
- 将音频转换为文字转录
- 使用AI生成详细的笔记摘要
- 支持自定义摘要比例
- 可单独使用音频转文字或文本摘要功能

## 安装

1. 安装FFmpeg：
   ```
   # macOS
   brew install ffmpeg

   # Ubuntu/Debian
   sudo apt-get install ffmpeg

   # Windows
   从https://ffmpeg.org/download.html下载并安装
   ```

2. 获取Go语言环境：
   从https://go.dev/dl/下载并安装Go

3. 下载并编译本项目：
   ```
   git clone https://github.com/yourusername/video-note-generator.git
   cd video-note-generator
   go build -o video-note .
   ```

## 使用方法

### 1. 配置API密钥
创建一个`config.json`文件，内容如下：{
  "openai_api_key": "你的OpenAI API密钥",
  "model": "gpt-3.5-turbo"
}
### 2. 生成视频笔记./video-note generate -i input_video.mp4 -o output_notes.txt
### 3. 其他命令
- 仅音频转文字：
  ```
  ./video-note transcribe -i audio.mp3 -o transcript.txt
  ```

- 仅生成文本摘要：
  ```
  ./video-note summarize -i transcript.txt -o summary.txt -ratio 0.3
  ```

## 命令行参数
- `-config`: 配置文件路径 (默认: config.json)
- `-i`: 输入文件路径
- `-o`: 输出文件路径
- `-ratio`: 摘要比例 (0.1-0.5, 默认: 0.2)

## 注意事项
- 需要有效的OpenAI API密钥
- 较大的视频文件可能需要较长的处理时间
- 确保系统有足够的存储空间用于临时文件    