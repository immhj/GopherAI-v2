package file

import (
	"log"
	"net/http"

	"GopherAI/common/code"
	"GopherAI/controller"
	"GopherAI/model"
	"GopherAI/service/file"

	"github.com/gin-gonic/gin"
)

type (
	UploadFileResponse struct {
		Document *model.Document `json:"document,omitempty"`
		controller.Response
	}

	ListDocumentsResponse struct {
		Documents []model.Document `json:"documents"`
		controller.Response
	}

	DeleteDocumentResponse struct {
		controller.Response
	}
)

// UploadRagFile 上传知识库文档（支持多份，不覆盖旧文档）
func UploadRagFile(c *gin.Context) {
	res := new(UploadFileResponse)
	uploadedFile, err := c.FormFile("file")
	if err != nil {
		log.Println("FormFile fail ", err)
		c.JSON(http.StatusOK, res.CodeOf(code.CodeInvalidParams))
		return
	}

	username := c.GetString("userName")
	if username == "" {
		log.Println("Username not found in context")
		c.JSON(http.StatusOK, res.CodeOf(code.CodeInvalidToken))
		return
	}

	doc, err := file.UploadRagFile(username, uploadedFile)
	if err != nil {
		log.Println("UploadFile fail ", err)
		c.JSON(http.StatusOK, res.CodeOf(code.CodeServerBusy))
		return
	}

	res.Success()
	res.Document = doc
	c.JSON(http.StatusOK, res)
}

// ListDocuments 列出当前用户的知识库文档
func ListDocuments(c *gin.Context) {
	res := new(ListDocumentsResponse)
	username := c.GetString("userName")
	if username == "" {
		c.JSON(http.StatusOK, res.CodeOf(code.CodeInvalidToken))
		return
	}

	docs, err := file.ListDocuments(username)
	if err != nil {
		log.Println("ListDocuments fail ", err)
		c.JSON(http.StatusOK, res.CodeOf(code.CodeServerBusy))
		return
	}

	res.Success()
	res.Documents = docs
	c.JSON(http.StatusOK, res)
}

// DeleteDocument 删除指定文档（含其向量）
func DeleteDocument(c *gin.Context) {
	res := new(DeleteDocumentResponse)
	username := c.GetString("userName")
	if username == "" {
		c.JSON(http.StatusOK, res.CodeOf(code.CodeInvalidToken))
		return
	}

	docID := c.Param("id")
	if docID == "" {
		c.JSON(http.StatusOK, res.CodeOf(code.CodeInvalidParams))
		return
	}

	if err := file.DeleteDocument(username, docID); err != nil {
		log.Println("DeleteDocument fail ", err)
		c.JSON(http.StatusOK, res.CodeOf(code.CodeServerBusy))
		return
	}

	res.Success()
	c.JSON(http.StatusOK, res)
}
