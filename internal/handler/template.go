package handler

import (
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/starlink/push/internal/app/template"
	"github.com/starlink/push/internal/domain"
	"github.com/starlink/push/pkg/errcode"
	"github.com/starlink/push/pkg/response"
)

type TemplateHandler struct {
	svc *template.Service
}

func NewTemplateHandler(svc *template.Service) *TemplateHandler {
	return &TemplateHandler{svc: svc}
}

func (h *TemplateHandler) Create(c *gin.Context) {
	var in domain.CreateTemplateInput
	if err := c.ShouldBindJSON(&in); err != nil {
		response.Fail(c, errcode.InvalidParam)
		return
	}
	tpl, err := h.svc.Create(c.Request.Context(), in)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, tpl)
}

func (h *TemplateHandler) List(c *gin.Context) {
	var q domain.ListTemplateQuery
	_ = c.ShouldBindQuery(&q)
	res, err := h.svc.List(c.Request.Context(), q)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, res)
}

func (h *TemplateHandler) Get(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		response.Fail(c, errcode.InvalidParam)
		return
	}
	tpl, err := h.svc.Get(c.Request.Context(), id)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, tpl)
}

func (h *TemplateHandler) GetByCode(c *gin.Context) {
	code := c.Param("code")
	tpl, err := h.svc.GetByCode(c.Request.Context(), code)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, tpl)
}

func (h *TemplateHandler) Update(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		response.Fail(c, errcode.InvalidParam)
		return
	}
	var in domain.UpdateTemplateInput
	if err := c.ShouldBindJSON(&in); err != nil {
		response.Fail(c, errcode.InvalidParam)
		return
	}
	tpl, err := h.svc.Update(c.Request.Context(), id, in)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, tpl)
}

func (h *TemplateHandler) Delete(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		response.Fail(c, errcode.InvalidParam)
		return
	}
	if err := h.svc.Delete(c.Request.Context(), id); err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, gin.H{"deleted": true})
}

func (h *TemplateHandler) Submit(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		response.Fail(c, errcode.InvalidParam)
		return
	}
	var body struct {
		Operator string `json:"operator"`
	}
	_ = c.ShouldBindJSON(&body)
	tpl, err := h.svc.Submit(c.Request.Context(), id, body.Operator)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, tpl)
}

func (h *TemplateHandler) Approve(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		response.Fail(c, errcode.InvalidParam)
		return
	}
	var in domain.ReviewTemplateInput
	_ = c.ShouldBindJSON(&in)
	tpl, err := h.svc.Approve(c.Request.Context(), id, in)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, tpl)
}

func (h *TemplateHandler) Reject(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		response.Fail(c, errcode.InvalidParam)
		return
	}
	var in domain.ReviewTemplateInput
	if err := c.ShouldBindJSON(&in); err != nil {
		response.Fail(c, errcode.InvalidParam)
		return
	}
	tpl, err := h.svc.Reject(c.Request.Context(), id, in)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, tpl)
}

func (h *TemplateHandler) Disable(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		response.Fail(c, errcode.InvalidParam)
		return
	}
	var body struct {
		Operator string `json:"operator"`
	}
	_ = c.ShouldBindJSON(&body)
	tpl, err := h.svc.Disable(c.Request.Context(), id, body.Operator)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, tpl)
}

func (h *TemplateHandler) Enable(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		response.Fail(c, errcode.InvalidParam)
		return
	}
	var body struct {
		Operator string `json:"operator"`
	}
	_ = c.ShouldBindJSON(&body)
	tpl, err := h.svc.Enable(c.Request.Context(), id, body.Operator)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, tpl)
}
