<template>
  <div class="ai-chat-container">
    <!-- 左侧：会话列表 + 知识库 -->
    <aside class="sidebar">
      <div class="sidebar-header">
        <button class="new-chat-btn" @click="createNewSession">＋ 新聊天</button>
      </div>

      <div class="sidebar-section">
        <div class="section-label">最近会话</div>
        <ul class="session-list-ul">
          <li
            v-for="session in sessions"
            :key="session.id"
            :class="['session-item', { active: currentSessionId === session.id }]"
            :title="session.name"
            @click="switchSession(session.id)"
          >
            {{ session.name || '新会话' }}
          </li>
        </ul>
      </div>

      <!-- 知识库文档：上传后 AI 会在需要时自行检索 -->
      <div class="doc-panel">
        <div class="section-label">
          知识库<span class="doc-count">{{ documents.length }}</span>
        </div>
        <p v-if="documents.length === 0" class="doc-empty">
          上传 .md / .txt，提问时 AI 会自行判断是否检索
        </p>
        <ul class="doc-list">
          <li v-for="doc in documents" :key="doc.id" class="doc-item">
            <span class="doc-name" :title="doc.filename">{{ doc.filename }}</span>
            <span class="doc-meta">{{ doc.chunk_count }}块</span>
            <button class="doc-del" title="删除" @click="deleteDocument(doc)">✕</button>
          </li>
        </ul>
      </div>
    </aside>

    <!-- 右侧聊天区域 -->
    <div class="chat-section">
      <header class="top-bar">
        <span class="app-name">GopherAI</span>
        <button class="logout-btn" @click="handleLogout">退出登录</button>
      </header>

      <div class="chat-messages" ref="messagesRef">
        <div
          v-for="(message, index) in currentMessages"
          :key="index"
          :class="['message', message.role === 'user' ? 'user-message' : 'ai-message']"
        >
          <div class="message-header">
            <b>{{ message.role === 'user' ? '你' : 'AI' }}:</b>
            <span v-if="message.meta && message.meta.status === 'streaming'" class="streaming-indicator"> ··</span>
          </div>
          <div v-if="message.tool" class="tool-status">🔧 调用工具：{{ message.tool }}</div>
          <div v-if="message.image" class="message-image">
            <img :src="message.image" alt="uploaded" />
          </div>
          <!-- 还没有收到第一个字之前，明确展示"思考中"，避免界面看起来毫无反应 -->
          <div
            v-if="message.role === 'assistant' && !message.content && (!message.meta || message.meta.status !== 'error')"
            class="thinking"
          >
            <span class="thinking-dots"><i></i><i></i><i></i></span>
            <span class="thinking-text">思考中…</span>
          </div>
          <div v-else class="message-content" v-html="renderMarkdown(message.content)"></div>
        </div>
      </div>

      <!-- 底部：工具条 + 输入区 -->
      <div class="composer">
        <div v-if="attachedImage" class="image-preview">
          <img :src="attachedImage" alt="preview" />
          <button class="image-remove" @click="attachedImage = ''">移除图片</button>
        </div>

        <div class="toolbar">
          <select v-model="selectedModel" class="model-select" title="选择模型">
            <option v-for="m in models" :key="m" :value="m">{{ m }}</option>
          </select>
          <div class="toolbar-right">
            <button class="tool-btn" @click="triggerFileUpload" :disabled="uploading">
              {{ uploading ? '索引中…' : '📎 文档' }}
            </button>
            <button class="tool-btn" @click="triggerImageUpload" :disabled="loading">🖼️ 图片</button>
          </div>
          <input
            ref="imageInput"
            type="file"
            accept="image/*"
            style="display: none"
            @change="handleImageSelect"
          />
          <input
            ref="fileInput"
            type="file"
            accept=".md,.txt,text/markdown,text/plain"
            style="display: none"
            @change="handleFileUpload"
          />
        </div>

        <div class="chat-input">
          <textarea
            v-model="inputMessage"
            placeholder="请输入你的问题…"
            @keydown.enter.exact.prevent="sendMessage"
            :disabled="loading"
            ref="messageInput"
            rows="1"
          ></textarea>
          <button
            type="button"
            :disabled="(!inputMessage.trim() && !attachedImage) || loading"
            @click="sendMessage"
            class="send-btn"
          >
            {{ loading ? '发送中…' : '发送' }}
          </button>
        </div>
      </div>
    </div>
  </div>
</template>

<script>


import { ref, nextTick, computed, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import api from '../utils/api'

export default {
  name: 'AIChat',
  setup() {
    const router = useRouter()

    const sessions = ref({})
    const currentSessionId = ref(null)
    const tempSession = ref(false)
    const currentMessages = ref([])
    const inputMessage = ref('')
    const loading = ref(false)
    const messagesRef = ref(null)
    const messageInput = ref(null)
    const selectedModel = ref('')
    const models = ref([])
    const imageInput = ref(null)
    const attachedImage = ref('')
    const fileInput = ref(null)
    const uploading = ref(false)
    const documents = ref([])


    const renderMarkdown = (text) => {
      if (!text && text !== '') return ''
      return String(text)
        .replace(/\*\*(.*?)\*\*/g, '<strong>$1</strong>')
        .replace(/\*(.*?)\*/g, '<em>$1</em>')
        .replace(/`(.*?)`/g, '<code>$1</code>')
        .replace(/\n/g, '<br>')
    }

    const loadSessions = async () => {
      try {
        const response = await api.get('/AI/chat/sessions')
        if (response.data && response.data.status_code === 1000 && Array.isArray(response.data.sessions)) {
          const sessionMap = {}
          response.data.sessions.forEach(s => {
            const sid = String(s.sessionId)
            sessionMap[sid] = {
              id: sid,
              name: s.name || `会话 ${sid}`,
              messages: [] // lazy load
            }
          })
          sessions.value = sessionMap
        }
      } catch (error) {
        console.error('Load sessions error:', error)
      }
    }

    const createNewSession = () => {
      currentSessionId.value = 'temp'
      tempSession.value = true
      currentMessages.value = []
      // focus input
      nextTick(() => {
        if (messageInput.value) messageInput.value.focus()
      })
    }

    const switchSession = async (sessionId) => {
      if (!sessionId) return
      currentSessionId.value = String(sessionId)
      tempSession.value = false

      // lazy load history if not present
      if (!sessions.value[sessionId].messages || sessions.value[sessionId].messages.length === 0) {
        try {
          const response = await api.post('/AI/chat/history', { sessionId: currentSessionId.value })
          if (response.data && response.data.status_code === 1000 && Array.isArray(response.data.history)) {
            const messages = response.data.history.map(item => ({
              role: item.is_user ? 'user' : 'assistant',
              content: item.content
            }))
            sessions.value[sessionId].messages = messages
          }
        } catch (err) {
          console.error('Load history error:', err)
        }
      }


      currentMessages.value = [...(sessions.value[sessionId].messages || [])]
      await nextTick()
      scrollToBottom()
    }

    const sendMessage = async () => {
      if ((!inputMessage.value || !inputMessage.value.trim()) && !attachedImage.value) {
        ElMessage.warning('请输入消息内容或添加图片')
        return
      }

      const currentInput = inputMessage.value
      const currentImage = attachedImage.value

      const userMessage = {
        role: 'user',
        content: currentInput,
        image: currentImage || undefined
      }
      inputMessage.value = ''
      attachedImage.value = ''

      currentMessages.value.push(userMessage)
      await nextTick()
      scrollToBottom()

      try {
        loading.value = true
        // 流式响应为默认且唯一的方式
        await handleStreaming(currentInput, currentImage)
      } catch (err) {
        console.error('Send message error:', err)
        ElMessage.error('发送失败，请重试')

        if (!tempSession.value && currentSessionId.value && sessions.value[currentSessionId.value] && sessions.value[currentSessionId.value].messages) {

          const sessionArr = sessions.value[currentSessionId.value].messages
          if (sessionArr && sessionArr.length) sessionArr.pop()
        }
        currentMessages.value.pop()
      } finally {
        await nextTick()
        scrollToBottom()
      }
    }


    async function handleStreaming(question, image) {

      const aiMessage = {
        role: 'assistant',
        content: '',
        meta: { status: 'streaming' } // mark streaming
      }


      const aiMessageIndex = currentMessages.value.length
      currentMessages.value.push(aiMessage)

      // 没有真实会话ID时一律走"新建会话"接口。
      // 只看 tempSession 是不够的：首次进入页面时它是 false 而 currentSessionId 为 null，
      // 那样会带着 sessionId=null 去打老会话接口，后端参数校验直接失败。
      const isNewSession =
        tempSession.value || !currentSessionId.value || currentSessionId.value === 'temp'

      if (!isNewSession && sessions.value[currentSessionId.value]) {
        if (!sessions.value[currentSessionId.value].messages) sessions.value[currentSessionId.value].messages = []
        sessions.value[currentSessionId.value].messages.push({ role: 'assistant', content: '' })
      }

      const url = isNewSession
        ? '/api/AI/chat/send-stream-new-session'
        : '/api/AI/chat/send-stream'

      const headers = {
        'Content-Type': 'application/json',
        'Authorization': `Bearer ${localStorage.getItem('token') || ''}`
      }

      const body = isNewSession
        ? { question: question, model: selectedModel.value, image: image || undefined }
        : { question: question, model: selectedModel.value, sessionId: currentSessionId.value, image: image || undefined }

      try {
        // 创建 fetch 连接读取 SSE 流
        const response = await fetch(url, {
          method: 'POST',
          headers,
          body: JSON.stringify(body)
        })

        if (!response.ok) {
          loading.value = false
          throw new Error('Network response was not ok')
        }

        // 后端在参数/鉴权出错时会返回 JSON（而且带着 200），不是 SSE 流。
        // 必须显式识别，否则前端读不到 data: 行，界面就会一片空白。
        const contentType = response.headers.get('content-type') || ''
        if (contentType.includes('application/json')) {
          const errPayload = await response.json()
          loading.value = false
          throw new Error(errPayload.error || errPayload.status_msg || '服务返回异常')
        }

        const reader = response.body.getReader()
        const decoder = new TextDecoder()
        let buffer = ''

        // 读取流数据
        // eslint-disable-next-line no-constant-condition
        while (true) {
          const { done, value } = await reader.read()
          if (done) break

          const chunk = decoder.decode(value, { stream: true })
          buffer += chunk

          // 按行分割
          const lines = buffer.split('\n')
          buffer = lines.pop() || '' // 保留未完成的行

          for (const rawLine of lines) {
            // 只去掉行尾的 \r，绝不能 trim 整行：文本增量的前后空格是有意义的
            const line = rawLine.endsWith('\r') ? rawLine.slice(0, -1) : rawLine
            if (!line.startsWith('data:')) continue

            // SSE 规范：冒号后紧跟的那一个空格属于分帧，只剥离这一个字符
            let data = line.slice(5)
            if (data.startsWith(' ')) data = data.slice(1)

            if (data === '[DONE]') {
              loading.value = false
              currentMessages.value[aiMessageIndex].meta = { status: 'done' }
              currentMessages.value = [...currentMessages.value]
              continue
            }

            // 所有事件都是 JSON：{content} / {sessionId} / {tool}
            let parsed
            try {
              parsed = JSON.parse(data)
            } catch (e) {
              console.warn('[SSE] skip non-JSON frame:', data)
              continue
            }

            if (typeof parsed.content === 'string') {
              // 直接按索引累加，保留原始空格与换行
              currentMessages.value[aiMessageIndex].content += parsed.content
            } else if (parsed.sessionId) {
              const newSid = String(parsed.sessionId)
              if (isNewSession) {
                sessions.value[newSid] = {
                  id: newSid,
                  // 先用截取的提问占位，后端生成好短标题后会通过 title 事件覆盖
                  name: question ? question.slice(0, 10) : '新会话',
                  messages: [...currentMessages.value]
                }
                currentSessionId.value = newSid
                tempSession.value = false
              }
            } else if (parsed.tool) {
              // agent 正在调用工具，展示状态给用户
              currentMessages.value[aiMessageIndex].tool = parsed.tool
            } else if (parsed.title) {
              // 后端生成好了会话短标题，实时更新侧边栏
              const sid = currentSessionId.value
              if (sid && sessions.value[sid]) {
                sessions.value[sid].name = parsed.title
              }
            }

            // 强制更新整个数组以触发响应式，并即时滚动到底部
            currentMessages.value = [...currentMessages.value]
            await new Promise(resolve => {
              requestAnimationFrame(() => {
                scrollToBottom()
                resolve()
              })
            })
          }
        }

        // 流读取完成后的处理
        loading.value = false
        currentMessages.value[aiMessageIndex].meta = { status: 'done' }
        currentMessages.value = [...currentMessages.value]

        // 同步到 sessions 存储
        if (!tempSession.value && currentSessionId.value && sessions.value[currentSessionId.value]) {
          const sessMsgs = sessions.value[currentSessionId.value].messages
          if (Array.isArray(sessMsgs) && sessMsgs.length) {
            const lastIndex = sessMsgs.length - 1
            if (sessMsgs[lastIndex] && sessMsgs[lastIndex].role === 'assistant') {
              sessMsgs[lastIndex].content = currentMessages.value[aiMessageIndex].content
            }
          }
        }
      } catch (err) {
        console.error('Stream error:', err)
        loading.value = false
        currentMessages.value[aiMessageIndex].meta = { status: 'error' }
        if (!currentMessages.value[aiMessageIndex].content) {
          currentMessages.value[aiMessageIndex].content = '⚠️ ' + (err.message || '请求失败')
        }
        currentMessages.value = [...currentMessages.value]
        ElMessage.error(err.message || '流式传输出错')
      }
    }


    const scrollToBottom = () => {
      if (messagesRef.value) {
        try {
          messagesRef.value.scrollTop = messagesRef.value.scrollHeight
        } catch (e) {
          // ignore
        }
      }
    }

    const loadModels = async () => {
      try {
        const response = await api.get('/AI/models')
        if (response.data && response.data.status_code === 1000 && Array.isArray(response.data.models)) {
          models.value = response.data.models
          selectedModel.value = response.data.defaultModel || response.data.models[0] || ''
        }
      } catch (error) {
        console.error('Load models error:', error)
      }
    }

    const triggerImageUpload = () => {
      if (imageInput.value) {
        imageInput.value.click()
      }
    }

    const handleImageSelect = (event) => {
      const file = event.target.files[0]
      if (!file) return
      if (!file.type.startsWith('image/')) {
        ElMessage.error('请选择图片文件')
        return
      }
      const reader = new FileReader()
      reader.onload = () => {
        attachedImage.value = reader.result // data:image/...;base64,xxx
      }
      reader.readAsDataURL(file)
      // 允许重复选择同一文件
      if (imageInput.value) {
        imageInput.value.value = ''
      }
    }

    const handleLogout = () => {
      localStorage.removeItem('token')
      ElMessage.success('已退出登录')
      router.push('/login')
    }

    const loadDocuments = async () => {
      try {
        const response = await api.get('/file/documents')
        if (response.data && response.data.status_code === 1000) {
          documents.value = response.data.documents || []
        }
      } catch (error) {
        console.error('Load documents error:', error)
      }
    }

    const triggerFileUpload = () => {
      if (fileInput.value) fileInput.value.click()
    }

    const handleFileUpload = async (event) => {
      const file = event.target.files[0]
      if (fileInput.value) fileInput.value.value = ''
      if (!file) return

      const name = file.name.toLowerCase()
      if (!name.endsWith('.md') && !name.endsWith('.txt')) {
        ElMessage.error('只允许上传 .md 或 .txt 文件')
        return
      }

      try {
        uploading.value = true
        const formData = new FormData()
        formData.append('file', file)
        // 上传是同步的：等切块 + 向量化 + 入库完成才返回
        const response = await api.post('/file/upload', formData, {
          headers: { 'Content-Type': 'multipart/form-data' }
        })
        if (response.data && response.data.status_code === 1000) {
          const chunks = response.data.document ? response.data.document.chunk_count : 0
          ElMessage.success(`已索引 ${chunks} 个片段`)
          await loadDocuments()
        } else {
          ElMessage.error(response.data?.status_msg || '上传失败')
        }
      } catch (error) {
        console.error('File upload error:', error)
        ElMessage.error('上传失败，请检查向量化服务是否可用')
      } finally {
        uploading.value = false
      }
    }

    const deleteDocument = async (doc) => {
      try {
        await ElMessageBox.confirm(`删除「${doc.filename}」？其向量也会一并清除。`, '提示', {
          confirmButtonText: '删除',
          cancelButtonText: '取消',
          type: 'warning'
        })
      } catch {
        return // 用户取消
      }
      try {
        const response = await api.delete(`/file/documents/${doc.id}`)
        if (response.data && response.data.status_code === 1000) {
          ElMessage.success('已删除')
          await loadDocuments()
        } else {
          ElMessage.error(response.data?.status_msg || '删除失败')
        }
      } catch (error) {
        console.error('Delete document error:', error)
        ElMessage.error('删除失败')
      }
    }

    onMounted(() => {
      loadModels()
      loadSessions()
      loadDocuments()
      // 进入页面即处于"新聊天"状态，用户可以直接输入并发送
      createNewSession()
    })

    // expose to template
    return {
      sessions: computed(() => Object.values(sessions.value)),
      currentSessionId,
      tempSession,
      currentMessages,
      inputMessage,
      loading,
      messagesRef,
      messageInput,
      selectedModel,
      models,
      imageInput,
      attachedImage,
      fileInput,
      uploading,
      documents,
      renderMarkdown,
      createNewSession,
      switchSession,
      sendMessage,
      triggerImageUpload,
      handleImageSelect,
      triggerFileUpload,
      handleFileUpload,
      deleteDocument,
      handleLogout
    }
  }
}
</script>

<style scoped>
/* 浅色主题：白色对话区 + 极浅灰侧边栏 + 单一强调色。
   不使用渐变、毛玻璃和装饰性动画。 */
.ai-chat-container {
  height: 100vh;
  display: flex;
  background: #ffffff;
  overflow: hidden;
  font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, "Helvetica Neue", Arial, sans-serif;
  color: #1f2328;
}

/* ---------------- 侧边栏 ---------------- */
.sidebar {
  width: 260px;
  flex-shrink: 0;
  display: flex;
  flex-direction: column;
  background: #f7f8fa;
  border-right: 1px solid #eceef2;
}

.sidebar-header {
  padding: 14px 14px 10px;
}

.new-chat-btn {
  width: 100%;
  padding: 9px 0;
  cursor: pointer;
  background: #2563eb;
  color: #ffffff;
  border: none;
  border-radius: 8px;
  font-size: 14px;
  font-weight: 500;
}

.new-chat-btn:hover {
  background: #1d4fd7;
}

.sidebar-section {
  flex: 1;
  min-height: 0;
  display: flex;
  flex-direction: column;
  padding: 0 8px;
}

.section-label {
  font-size: 12px;
  color: #8b949e;
  padding: 8px 6px 6px;
  display: flex;
  align-items: center;
  gap: 6px;
}

.session-list-ul {
  list-style: none;
  padding: 0;
  margin: 0;
  flex: 1;
  min-height: 0;
  overflow-y: auto;
}

.session-item {
  padding: 8px 10px;
  margin-bottom: 2px;
  cursor: pointer;
  border-radius: 6px;
  font-size: 14px;
  color: #3a4149;
  /* 标题过长时省略，避免撑破侧边栏 */
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.session-item:hover {
  background: #eceef2;
}

.session-item.active {
  background: #e7effd;
  color: #2563eb;
  font-weight: 500;
}

/* ---------------- 知识库 ---------------- */
.doc-panel {
  border-top: 1px solid #eceef2;
  padding: 6px 14px 14px;
  max-height: 40%;
  overflow-y: auto;
}

.doc-count {
  background: #e1e4e8;
  color: #57606a;
  border-radius: 9px;
  padding: 0 6px;
  font-size: 11px;
  line-height: 17px;
}

.doc-empty {
  font-size: 12px;
  color: #a0a6ad;
  line-height: 1.5;
  margin: 0 0 0 6px;
}

.doc-list {
  list-style: none;
  padding: 0;
  margin: 0;
}

.doc-item {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 5px 6px;
  font-size: 12px;
  color: #3a4149;
  border-radius: 6px;
}

.doc-item:hover {
  background: #eceef2;
}

.doc-name {
  flex: 1;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.doc-meta {
  color: #a0a6ad;
  flex-shrink: 0;
}

.doc-del {
  border: none;
  background: transparent;
  color: #b0b6bd;
  cursor: pointer;
  font-size: 12px;
  padding: 0 2px;
  flex-shrink: 0;
}

.doc-del:hover {
  color: #d1242f;
}

/* ---------------- 聊天区 ---------------- */
.chat-section {
  flex: 1;
  min-width: 0;
  min-height: 0;
  display: flex;
  flex-direction: column;
  background: #ffffff;
}

.top-bar {
  height: 52px;
  flex-shrink: 0;
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 0 20px;
  border-bottom: 1px solid #eceef2;
}

.app-name {
  font-size: 15px;
  font-weight: 600;
  color: #1f2328;
}

.logout-btn {
  background: transparent;
  border: 1px solid #d8dce1;
  color: #57606a;
  padding: 6px 12px;
  border-radius: 7px;
  cursor: pointer;
  font-size: 13px;
}

.logout-btn:hover {
  background: #f2f4f7;
  color: #1f2328;
}

.chat-messages {
  flex: 1;
  min-height: 0;
  overflow-y: auto;
  padding: 24px 28px;
  display: flex;
  flex-direction: column;
  gap: 14px;
}

.chat-messages::-webkit-scrollbar {
  width: 8px;
}

.chat-messages::-webkit-scrollbar-thumb {
  background: #dfe3e8;
  border-radius: 8px;
}

.chat-messages::-webkit-scrollbar-track {
  background: transparent;
}

/* ---------------- 消息气泡 ---------------- */
.message {
  max-width: 74%;
  padding: 11px 15px;
  border-radius: 12px;
  line-height: 1.65;
  font-size: 15px;
  word-wrap: break-word;
  box-sizing: border-box;
}

.user-message {
  align-self: flex-end;
  background: #eef2f8;
  color: #1f2328;
}

.ai-message {
  align-self: flex-start;
  background: #ffffff;
  color: #1f2328;
  border: 1px solid #eceef2;
}

.message-header {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-bottom: 5px;
  font-size: 12px;
  color: #8b949e;
}

.message-header b {
  font-weight: 500;
}

.streaming-indicator {
  color: #a0a6ad;
}

.message-content {
  white-space: pre-wrap;
  word-break: break-word;
}

.message-image img {
  max-width: 100%;
  max-height: 260px;
  border-radius: 8px;
  margin-bottom: 8px;
  display: block;
}

/* 思考中指示器 */
.thinking {
  display: flex;
  align-items: center;
  gap: 9px;
}

.thinking-text {
  color: #8b949e;
  font-size: 14px;
}

.thinking-dots {
  display: inline-flex;
  gap: 4px;
}

.thinking-dots i {
  width: 6px;
  height: 6px;
  border-radius: 50%;
  background: #2563eb;
  display: inline-block;
  animation: thinkingBounce 1.3s infinite ease-in-out both;
}

.thinking-dots i:nth-child(1) { animation-delay: -0.32s; }
.thinking-dots i:nth-child(2) { animation-delay: -0.16s; }

@keyframes thinkingBounce {
  0%, 80%, 100% { transform: scale(0.6); opacity: 0.45; }
  40% { transform: scale(1); opacity: 1; }
}

/* agent 工具调用状态 */
.tool-status {
  font-size: 12px;
  color: #57606a;
  background: #f2f4f7;
  border-left: 2px solid #2563eb;
  padding: 4px 9px;
  border-radius: 4px;
  margin-bottom: 7px;
}

/* ---------------- 底部：工具条 + 输入区 ---------------- */
.composer {
  flex-shrink: 0;
  border-top: 1px solid #eceef2;
  padding: 12px 28px 18px;
}

.image-preview {
  display: flex;
  align-items: center;
  gap: 10px;
  margin-bottom: 10px;
}

.image-preview img {
  max-height: 60px;
  max-width: 100px;
  border-radius: 6px;
  border: 1px solid #eceef2;
  object-fit: cover;
}

.image-remove {
  padding: 5px 10px;
  border: 1px solid #d8dce1;
  border-radius: 6px;
  background: #ffffff;
  color: #57606a;
  cursor: pointer;
  font-size: 12px;
}

.image-remove:hover {
  color: #d1242f;
  border-color: #f0c2c5;
}

.toolbar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
  margin-bottom: 10px;
}

.toolbar-right {
  display: flex;
  gap: 8px;
}

.model-select {
  padding: 6px 10px;
  border: 1px solid #d8dce1;
  border-radius: 7px;
  background: #ffffff;
  color: #1f2328;
  font-size: 13px;
  cursor: pointer;
  outline: none;
}

.model-select:focus {
  border-color: #2563eb;
}

.tool-btn {
  background: #ffffff;
  border: 1px solid #d8dce1;
  color: #57606a;
  padding: 6px 12px;
  border-radius: 7px;
  cursor: pointer;
  font-size: 13px;
}

.tool-btn:hover:not(:disabled) {
  background: #f2f4f7;
  color: #1f2328;
}

.tool-btn:disabled {
  color: #b0b6bd;
  cursor: not-allowed;
}

.chat-input {
  position: relative;
}

.chat-input textarea {
  width: 100%;
  resize: none;
  border: 1px solid #d8dce1;
  border-radius: 10px;
  padding: 12px 92px 12px 14px;
  font-size: 15px;
  font-family: inherit;
  outline: none;
  background: #ffffff;
  color: #1f2328;
  min-height: 22px;
  max-height: 160px;
  box-sizing: border-box;
  line-height: 1.5;
}

.chat-input textarea:focus {
  border-color: #2563eb;
}

.chat-input textarea::placeholder {
  color: #a0a6ad;
}

.send-btn {
  position: absolute;
  right: 8px;
  bottom: 8px;
  padding: 7px 16px;
  border: none;
  border-radius: 7px;
  background: #2563eb;
  color: #ffffff;
  font-size: 14px;
  font-weight: 500;
  cursor: pointer;
}

.send-btn:hover:not(:disabled) {
  background: #1d4fd7;
}

.send-btn:disabled {
  background: #c8ccd1;
  cursor: not-allowed;
}
</style>
