# LongCat API Wrapper

[![Go Version](https://img.shields.io/badge/go-1.21+-blue.svg)](https://golang.org)
[![License](https://img.shields.io/badge/license-MIT-green.svg)](LICENSE)

OpenAI and Claude API compatible wrapper for LongCat Chat service. This allows you to use LongCat with any OpenAI or Claude API compatible client.

## 🚀 Features

- ✅ OpenAI API compatibility (`/v1/chat/completions`)
- ✅ Claude API compatibility (`/v1/messages`)
- ✅ Streaming and non-streaming responses
- ✅ Conversation history management
- ✅ Interactive cookie configuration
- ✅ Secure cookie storage
- ✅ CORS support for web applications
- ✅ Verbose logging mode

## 📋 Table of Contents

- [Quick Start](#quick-start)
- [Installation](#installation)
- [Configuration](#configuration)
- [API Usage](#api-usage)
- [Developer Guide](#developer-guide)
- [Troubleshooting](#troubleshooting)
- [Contributing](#contributing)
- [License](#license)

## 🚀 Quick Start

### Prerequisites
- Go 1.21 or higher
- LongCat Chat account

### 1. Build the application
```bash
go build -o longcat-web-api
```

### 2. Run the server
```bash
./longcat-web-api
```

**First Run Setup:**
On first run, if no cookies are configured, you'll be prompted to provide them:
```
=== Cookie Configuration Required ===

To get your cookies:
1. Open https://longcat.chat in your browser and login
2. Open Developer Tools (F12)
3. Go to Application/Storage → Cookies → https://longcat.chat
4. Find these cookies and copy their values

Paste your cookies here and press Enter:
> _lxsdk_cuid=xxx; passport_token_key=yyy; _lxsdk_s=zzz
```

The server will start on port 8082 by default.

## 📦 Installation

### From Source
```bash
git clone https://github.com/JessonChan/longcat-web-api.git
cd longcat-web-api
go build -o longcat-web-api
```

### Using Go Install
```bash
go install github.com/JessonChan/longcat-web-api@latest
```

## ⚙️ Configuration

### Cookie Configuration

#### Method 1: Interactive Setup (Recommended)
Simply run the application and paste your cookies when prompted. They'll be saved securely for future use.

#### Method 2: Environment Variables
Set these in your `.env` file or environment:
```bash
COOKIE_LXSDK_CUID=your_cuid_value
COOKIE_PASSPORT_TOKEN=your_token_value  # Required
COOKIE_LXSDK_S=your_s_value
```

#### Method 3: Saved Configuration
Cookies are automatically saved to `~/.config/longcat-web-api/config.json` when you choose to save them during interactive setup.

### Environment Variables

| Variable | Description | Default |
|----------|-------------|---------|
| `SERVER_PORT` | Server port | 8082 |
| `LONGCAT_API_URL` | LongCat API endpoint | (built-in) |
| `TIMEOUT_SECONDS` | Request timeout | 30 |
| `COOKIE_LXSDK_CUID` | LongCat session cookie | - |
| `COOKIE_PASSPORT_TOKEN` | LongCat auth token (required) | - |
| `COOKIE_LXSDK_S` | LongCat tracking cookie | - |

## 🛠️ Command-Line Options

```bash
# Show help
./longcat-web-api -h

# Update stored cookies
./longcat-web-api -update-cookies

# Clear stored cookies
./longcat-web-api -clear-cookies

# Show version
./longcat-web-api -version

# Enable verbose logging
./longcat-web-api -verbose
```

## 🔌 API Usage

### OpenAI Compatible API

#### Basic Chat Completion
```bash
curl http://localhost:8082/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{
    "model": "gpt-4",
    "messages": [
      {"role": "user", "content": "Hello! How are you?"}
    ],
    "stream": false
  }'
```

#### Streaming Response
```bash
curl http://localhost:8082/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{
    "model": "gpt-4",
    "messages": [
      {"role": "system", "content": "You are a helpful assistant."},
      {"role": "user", "content": "Explain quantum computing in simple terms."}
    ],
    "stream": true
  }'
```

### Claude Compatible API

#### Basic Message
```bash
curl http://localhost:8082/v1/messages \
  -H "Content-Type: application/json" \
  -H "anthropic-version: 2023-06-01" \
  -d '{
    "model": "claude-3",
    "max_tokens": 1000,
    "messages": [
      {"role": "user", "content": "Hello! How are you?"}
    ]
  }'
```

#### With System Message
```bash
curl http://localhost:8082/v1/messages \
  -H "Content-Type: application/json" \
  -H "anthropic-version: 2023-06-01" \
  -d '{
    "model": "claude-3",
    "max_tokens": 1000,
    "system": "You are a helpful assistant that responds in a friendly tone.",
    "messages": [
      {"role": "user", "content": "What is the meaning of life?"}
    ],
    "stream": true
  }'
```

### Python Client Example

```python
import openai

# Configure OpenAI client to use LongCat wrapper
client = openai.OpenAI(
    api_key="not-needed",  # No API key required for local wrapper
    base_url="http://localhost:8082/v1"
)

# Non-streaming chat completion
response = client.chat.completions.create(
    model="gpt-4",
    messages=[
        {"role": "user", "content": "Hello! Can you help me with Go programming?"}
    ]
)
print(response.choices[0].message.content)

# Streaming chat completion
stream = client.chat.completions.create(
    model="gpt-4",
    messages=[{"role": "user", "content": "Tell me a story"}],
    stream=True
)
for chunk in stream:
    if chunk.choices[0].delta.content:
        print(chunk.choices[0].delta.content, end="")
```

### JavaScript/Node.js Example

```javascript
const OpenAI = require('openai');

const openai = new OpenAI({
  baseURL: 'http://localhost:8082/v1',
  apiKey: 'not-needed' // No API key required for local wrapper
});

async function chat() {
  const completion = await openai.chat.completions.create({
    model: 'gpt-4',
    messages: [
      { role: 'user', content: 'Hello! How can you help me?' }
    ]
  });
  
  console.log(completion.choices[0].message.content);
}

chat();
```

## 🔑 Getting Cookies from Browser

1. Open https://longcat.chat and login
2. Open Developer Tools (F12)
3. Go to Application tab → Storage → Cookies
4. Find and copy these cookie values:
   - `_lxsdk_cuid`
   - `passport_token_key` (required)
   - `_lxsdk_s`

You can copy them individually or as a complete cookie string.

## 👨‍💻 Developer Guide

### Project Structure

```
longcat-web-api/
├── main.go                 # Main application entry point
├── api/                    # API service implementations
│   ├── openai.go          # OpenAI API compatibility
│   ├── claude.go          # Claude API compatibility
│   └── client.go          # LongCat API client
├── config/                # Configuration management
├── types/                 # Type definitions
├── conversation/          # Conversation management
└── logging/              # Logging utilities
```

### Development Setup

1. **Clone the repository:**
   ```bash
   git clone https://github.com/JessonChan/longcat-web-api.git
   cd longcat-web-api
   ```

2. **Install dependencies:**
   ```bash
   go mod tidy
   ```

3. **Run in development mode:**
   ```bash
   go run main.go -verbose
   ```

### Building

```bash
# Build for current platform
go build -o longcat-web-api

# Build for multiple platforms
make build-all
```

### Testing

```bash
# Run all tests
go test ./...

# Run tests with verbose output
go test -v ./...

# Run tests with coverage
go test -cover ./...
```

### Contributing

1. Fork the repository
2. Create a feature branch: `git checkout -b feature/amazing-feature`
3. Commit your changes: `git commit -m 'Add amazing feature'`
4. Push to the branch: `git push origin feature/amazing-feature`
5. Open a Pull Request

#### Code Style

- Follow Go standard formatting (`go fmt`)
- Use conventional commits
- Add tests for new features
- Update documentation as needed

## 🚨 Troubleshooting

### Common Issues

#### Authentication Failed
**Error:** `Failed to authenticate with LongCat`

**Solutions:**
1. Update your cookies: `./longcat-web-api -update-cookies`
2. Ensure cookies haven't expired (re-login to LongCat if needed)
3. Verify you're copying the complete cookie values
4. Check that the config file has proper permissions

#### Port Already in Use
**Error:** `bind: address already in use`

**Solutions:**
1. Change the port: `export SERVER_PORT=8083`
2. Kill the process using the port: `lsof -ti:8082 | xargs kill -9`

#### Build Errors
**Error:** Various Go compilation errors

**Solutions:**
1. Ensure you have Go 1.21 or higher: `go version`
2. Clean and rebuild: `go clean && go build`
3. Update dependencies: `go mod tidy`

#### Cookie Configuration Issues
**Error:** No cookies found or invalid cookies

**Solutions:**
1. Clear saved cookies: `./longcat-web-api -clear-cookies`
2. Reconfigure cookies: `./longcat-web-api -update-cookies`
3. Check environment variables are set correctly

### FAQ

**Q: Do I need an API key?**
A: No, you just need your LongCat session cookies from the browser.

**Q: Can I use this with any OpenAI/Claude client?**
A: Yes, it's compatible with any client that supports OpenAI or Claude API formats.

**Q: How do I update my cookies when they expire?**
A: Run `./longcat-web-api -update-cookies` and provide new cookies from your browser.

**Q: Is my conversation history saved?**
A: Conversation history is managed in-memory only during the server session.

**Q: Can I run this on a different port?**
A: Yes, set the `SERVER_PORT` environment variable: `export SERVER_PORT=3000`

## 🔒 Security Notes

- Cookies are stored with 0600 permissions (owner read/write only)
- Cookie values are masked when displayed
- The `passport_token_key` is required for authentication
- Keep your cookies secure and don't share them
- The server runs locally by default - be cautious when exposing it to networks

## 🤝 Contributing

Contributions are welcome! Please feel free to submit a Pull Request. For major changes, please open an issue first to discuss what you would like to change.

## 📄 License

This project is licensed under the MIT License - see the [LICENSE](LICENSE) file for details.