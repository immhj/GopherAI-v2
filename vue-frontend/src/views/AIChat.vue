<template>
  <div class="ai-chat-container">
    <!-- 左侧会话列表 -->
    <div class="session-list">
      <div class="session-list-header">
        <span>会话列表</span>
        <button class="new-chat-btn" @click="createNewSession">＋ 新聊天</button>
      </div>
      <ul class="session-list-ul">
        <li
          v-for="session in sessions"
          :key="session.id"
          :class="['session-item', { active: currentSessionId === session.id }]"
          @click="switchSession(session.id)"
        >
          {{ session.name || `会话 ${session.id}` }}
        </li>
      </ul>

      <!-- 知识库文档：上传后 AI 会在需要时自行检索 -->
      <div class="doc-panel">
        <div class="doc-panel-title">
          知识库<span class="doc-count">{{ documents.length }}</span>
        </div>
        <p v-if="documents.length === 0" class="doc-empty">
          上传 .md / .txt，提问时 AI 会自行判断是否检索
        </p>
        <ul class="doc-list">
          <li v-for="doc in documents" :key="doc.id" class="doc-item">
            <span class="doc-name" :title="doc.filename">{{ doc.filename }}</span>
            <span class="doc-meta">{{ doc.chunk_count }} 块</span>
            <button class="doc-del" title="删除" @click="deleteDocument(doc)">✕</button>
          </li>
        </ul>
      </div>
    </div>

    <!-- 右侧聊天区域 -->
    <div class="chat-section">
      <div class="top-bar">
        <button class="back-btn" @click="handleLogout">退出登录</button>
        <label for="modelType">模型：</label>
        <select id="modelType" v-model="selectedModel" class="model-select">
          <option v-for="m in models" :key="m" :value="m">{{ m }}</option>
        </select>
        <button class="upload-btn" @click="triggerImageUpload" :disabled="loading">🖼️ 图片</button>
        <button class="upload-btn" @click="triggerFileUpload" :disabled="uploading">
          {{ uploading ? '索引中…' : '📎 文档' }}
        </button>
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

      <div v-if="attachedImage" class="image-preview">
        <img :src="attachedImage" alt="preview" />
        <button class="image-remove" @click="attachedImage = ''">✕ 移除图片</button>
      </div>

      <div class="chat-input">
        <textarea
          v-model="inputMessage"
          placeholder="请输入你的问题...（可附带图片）"
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
          {{ loading ? '发送中...' : '发送' }}
        </button>
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
                  name: question ? question.slice(0, 20) : '新会话',
                  messages: [...currentMessages.value]
                }
                currentSessionId.value = newSid
                tempSession.value = false
              }
            } else if (parsed.tool) {
              // agent 正在调用工具，展示状态给用户
              currentMessages.value[aiMessageIndex].tool = parsed.tool
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
.ai-chat-container {
  height: 100vh;
  display: flex;
  background: linear-gradient(135deg, #667eea 0%, #764ba2 100%);
  position: relative;
  overflow: hidden;
  font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, "Helvetica Neue", Arial;
  color: #222;
}

.ai-chat-container::before {
  content: '';
  position: absolute;
  top: 0;
  left: 0;
  right: 0;
  bottom: 0;
  background: url('data:image/svg+xml,<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 100 100"><circle cx="20" cy="20" r="2" fill="rgba(255,255,255,0.08)"/><circle cx="80" cy="80" r="2" fill="rgba(255,255,255,0.08)"/><circle cx="40" cy="60" r="1" fill="rgba(255,255,255,0.06)"/><circle cx="60" cy="30" r="1.5" fill="rgba(255,255,255,0.06)"/></svg>');
  animation: float 20s ease-in-out infinite;
  opacity: 0.25;
}

@keyframes float {
  0%, 100% { transform: translateY(0px) rotate(0deg); }
  50% { transform: translateY(-20px) rotate(180deg); }
}

.session-list {
  width: 280px;
  height: 100vh;
  overflow: hidden;
  display: flex;
  flex-direction: column;
  background: rgba(255, 255, 255, 0.95);
  backdrop-filter: blur(15px);
  border-right: 1px solid rgba(0, 0, 0, 0.08);
  box-shadow: 2px 0 20px rgba(0, 0, 0, 0.08);
  position: relative;
  z-index: 2;
}

.session-list-header {
  padding: 20px;
  text-align: center;
  font-weight: 600;
  background: linear-gradient(135deg, rgba(102, 126, 234, 0.06) 0%, rgba(103, 194, 58, 0.06) 100%);
  border-bottom: 1px solid rgba(0, 0, 0, 0.06);
  display: flex;
  flex-direction: column;
  gap: 12px;
  align-items: center;
}

.new-chat-btn {
  width: 100%;
  padding: 12px 0;
  cursor: pointer;
  background: linear-gradient(135deg, #667eea 0%, #764ba2 100%);
  color: white;
  border: none;
  border-radius: 12px;
  font-size: 14px;
  font-weight: 600;
  box-shadow: 0 4px 15px rgba(102, 126, 234, 0.28);
  transition: all 0.25s ease;
  position: relative;
  overflow: hidden;
}

.new-chat-btn::before {
  content: '';
  position: absolute;
  top: 0;
  left: -100%;
  width: 100%;
  height: 100%;
  background: linear-gradient(90deg, transparent, rgba(255,255,255,0.12), transparent);
  transition: left 0.5s;
}

.new-chat-btn:hover::before {
  left: 100%;
}

.new-chat-btn:hover {
  transform: translateY(-2px);
  box-shadow: 0 8px 25px rgba(102, 126, 234, 0.36);
}

.session-list-ul {
  list-style: none;
  padding: 0;
  margin: 0;
  flex: 1;
  overflow-y: auto;
}

/* 知识库文档面板 */
.doc-panel {
  border-top: 1px solid rgba(0, 0, 0, 0.08);
  padding: 14px 16px;
  max-height: 38%;
  overflow-y: auto;
  background: rgba(102, 126, 234, 0.03);
}

.doc-panel-title {
  font-size: 13px;
  font-weight: 600;
  color: #2c3e50;
  margin-bottom: 8px;
  display: flex;
  align-items: center;
  gap: 6px;
}

.doc-count {
  background: #667eea;
  color: white;
  border-radius: 10px;
  padding: 0 7px;
  font-size: 11px;
  line-height: 18px;
}

.doc-empty {
  font-size: 12px;
  color: #95a5a6;
  line-height: 1.5;
  margin: 0;
}

.doc-list {
  list-style: none;
  padding: 0;
  margin: 0;
}

.doc-item {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 6px 0;
  font-size: 12px;
  color: #2c3e50;
}

.doc-name {
  flex: 1;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.doc-meta {
  color: #95a5a6;
  flex-shrink: 0;
}

.doc-del {
  border: none;
  background: transparent;
  color: #c0392b;
  cursor: pointer;
  font-size: 13px;
  padding: 0 2px;
  flex-shrink: 0;
}

.doc-del:hover {
  color: #e74c3c;
}

.session-item {
  padding: 15px 20px;
  cursor: pointer;
  border-bottom: 1px solid rgba(0, 0, 0, 0.03);
  transition: all 0.2s ease;
  position: relative;
  color: #2c3e50;
}

.session-item.active {
  background: linear-gradient(135deg, #667eea 0%, #764ba2 100%);
  color: white;
  font-weight: 600;
  box-shadow: inset 0 0 20px rgba(102, 126, 234, 0.2);
}

.session-item:hover {
  background: rgba(102, 126, 234, 0.06);
  transform: translateX(4px);
}

/* chat section */
.chat-section {
  flex: 1;
  display: flex;
  flex-direction: column;
  position: relative;
  z-index: 1;
  min-width: 0;
  min-height: 0;
  overflow: hidden;
}

.top-bar {
  background: rgba(255, 255, 255, 0.95);
  backdrop-filter: blur(10px);
  color: #2c3e50;
  display: flex;
  align-items: center;
  padding: 12px 24px;
  box-shadow: 0 2px 14px rgba(0, 0, 0, 0.06);
  border-bottom: 1px solid rgba(0, 0, 0, 0.06);
  gap: 12px;
}

.back-btn {
  background: rgba(255, 255, 255, 0.22);
  border: 1px solid rgba(0, 0, 0, 0.06);
  color: #2c3e50;
  padding: 8px 14px;
  border-radius: 10px;
  cursor: pointer;
  font-weight: 600;
  transition: all 0.2s ease;
  box-shadow: 0 2px 8px rgba(0, 0, 0, 0.06);
}

.back-btn:hover {
  background: rgba(255, 255, 255, 0.32);
  transform: translateY(-2px);
  box-shadow: 0 6px 20px rgba(0, 0, 0, 0.08);
}

.model-select {
  margin-left: 6px;
  padding: 6px 10px;
  border: 1px solid rgba(0, 0, 0, 0.06);
  border-radius: 8px;
  background: white;
  color: #2c3e50;
  font-weight: 600;
  cursor: pointer;
  transition: all 0.2s ease;
}

.upload-btn {
  background: linear-gradient(135deg, #f093fb 0%, #f5576c 100%);
  color: white;
  padding: 8px 14px;
  border: none;
  border-radius: 10px;
  cursor: pointer;
  font-size: 13px;
  font-weight: 600;
  box-shadow: 0 4px 12px rgba(245, 87, 108, 0.2);
  transition: all 0.2s ease;
}

.upload-btn:hover:not(:disabled) {
  transform: translateY(-2px);
  box-shadow: 0 6px 16px rgba(245, 87, 108, 0.3);
}

.upload-btn:disabled {
  background: #ccc;
  box-shadow: none;
  cursor: not-allowed;
}

.chat-messages {
  flex: 1;
  min-height: 0;
  overflow-y: auto;
  padding: 30px;
  display: flex;
  flex-direction: column;
  gap: 18px;
  position: relative;
  z-index: 1;
}

/* scrollbar */
.chat-messages::-webkit-scrollbar {
  width: 8px;
}
.chat-messages::-webkit-scrollbar-thumb {
  background: rgba(0,0,0,0.12);
  border-radius: 8px;
}
.chat-messages::-webkit-scrollbar-track {
  background: transparent;
}

.message {
  max-width: 70%;
  padding: 14px 18px;
  border-radius: 18px;
  line-height: 1.6;
  word-wrap: break-word;
  position: relative;
  animation: messageSlideIn 0.28s ease-out;
  font-size: 15px;
  box-sizing: border-box;
}

@keyframes messageSlideIn {
  from {
    opacity: 0;
    transform: translateY(12px) scale(0.98);
  }
  to {
    opacity: 1;
    transform: translateY(0) scale(1);
  }
}

.user-message {
  align-self: flex-end;
  background: linear-gradient(135deg, #667eea 0%, #764ba2 100%);
  color: white;
  box-shadow: 0 6px 20px rgba(102, 126, 234, 0.16);
}

.user-message::after {
  content: '';
  position: absolute;
  bottom: -6px;
  right: 18px;
  width: 0;
  height: 0;
  border-left: 8px solid transparent;
  border-right: 8px solid transparent;
  border-top: 8px solid #764ba2;
}

.ai-message {
  align-self: flex-start;
  background: rgba(255, 255, 255, 0.95);
  backdrop-filter: blur(4px);
  color: #2c3e50;
  box-shadow: 0 6px 20px rgba(0, 0, 0, 0.06);
  border: 1px solid rgba(255, 255, 255, 0.3);
}

.ai-message::after {
  content: '';
  position: absolute;
  bottom: -6px;
  left: 18px;
  width: 0;
  height: 0;
  border-left: 8px solid transparent;
  border-right: 8px solid transparent;
  border-top: 8px solid rgba(255, 255, 255, 0.95);
}

.message-header {
  display: flex;
  align-items: center;
  gap: 10px;
  margin-bottom: 8px;
}

.message-header b {
  font-weight: 600;
}

/* 思考中指示器 */
.thinking {
  display: flex;
  align-items: center;
  gap: 10px;
}

.thinking-text {
  color: #7f8c8d;
  font-size: 14px;
}

.thinking-dots {
  display: inline-flex;
  gap: 4px;
}

.thinking-dots i {
  width: 7px;
  height: 7px;
  border-radius: 50%;
  background: #667eea;
  display: inline-block;
  animation: thinkingBounce 1.3s infinite ease-in-out both;
}

.thinking-dots i:nth-child(1) { animation-delay: -0.32s; }
.thinking-dots i:nth-child(2) { animation-delay: -0.16s; }

@keyframes thinkingBounce {
  0%, 80%, 100% { transform: scale(0.6); opacity: 0.5; }
  40% { transform: scale(1); opacity: 1; }
}

/* agent 工具调用状态 */
.tool-status {
  font-size: 12px;
  color: #7f8c8d;
  background: rgba(102, 126, 234, 0.08);
  border-left: 3px solid #667eea;
  padding: 4px 10px;
  border-radius: 6px;
  margin-bottom: 8px;
}

.streaming-indicator {
  color: #999;
  font-weight: 600;
  margin-left: 6px;
}

/* message content */
.message-content {
  white-space: pre-wrap;
  word-break: break-word;
}

/* input area */
.chat-input {
  padding: 24px;
  background: rgba(255, 255, 255, 0.96);
  backdrop-filter: blur(8px);
  border-top: 1px solid rgba(0, 0, 0, 0.06);
  position: relative;
  z-index: 1;
}

.chat-input textarea {
  width: 100%;
  resize: none;
  border: 2px solid rgba(0, 0, 0, 0.06);
  border-radius: 12px;
  padding: 14px 16px;
  font-size: 15px;
  outline: none;
  background: rgba(255,255,255,0.96);
  color: #2c3e50;
  transition: all 0.18s ease;
  min-height: 20px;
  max-height: 160px;
  box-shadow: 0 2px 10px rgba(0,0,0,0.04);
}

.chat-input textarea:focus {
  border-color: #409eff;
  box-shadow: 0 8px 30px rgba(64,158,255,0.06);
  transform: translateY(-1px);
}

.send-btn {
  position: absolute;
  right: 36px;
  bottom: 30px;
  padding: 12px 22px;
  border: none;
  border-radius: 50px;
  background: linear-gradient(135deg, #667eea 0%, #764ba2 100%);
  color: white;
  font-size: 15px;
  font-weight: 600;
  cursor: pointer;
  box-shadow: 0 6px 20px rgba(102,126,234,0.18);
  transition: all 0.18s ease;
}

.send-btn:hover:not(:disabled) {
  transform: translateY(-3px) scale(1.02);
}

.send-btn:disabled {
  background: #ccc;
  box-shadow: none;
  cursor: not-allowed;
}

/* attached image preview above input */
.image-preview {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 10px 24px;
  background: rgba(255, 255, 255, 0.9);
  border-top: 1px solid rgba(0, 0, 0, 0.06);
}

.image-preview img {
  max-height: 72px;
  max-width: 120px;
  border-radius: 8px;
  border: 1px solid rgba(0, 0, 0, 0.08);
  object-fit: cover;
}

.image-remove {
  padding: 6px 12px;
  border: none;
  border-radius: 8px;
  background: #f5576c;
  color: white;
  cursor: pointer;
  font-size: 13px;
}

/* image inside a chat message */
.message-image img {
  max-width: 100%;
  max-height: 260px;
  border-radius: 12px;
  margin-bottom: 8px;
  display: block;
}
</style>
