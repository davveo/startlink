package handler

import (
	"net/http"
	"os"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/starlink/push/internal/domain"
	"github.com/starlink/push/pkg/errcode"
	"github.com/starlink/push/pkg/response"
)

func (h *CampaignHandler) Batch(c *gin.Context) {
	action := c.Param("action")
	var in domain.BatchActionInput
	if err := c.ShouldBindJSON(&in); err != nil || len(in.IDs) == 0 {
		response.Fail(c, errcode.InvalidParam)
		return
	}
	if len(in.IDs) > 100 {
		response.Fail(c, errcode.InvalidParam)
		return
	}
	res, err := h.svc.BatchAction(c.Request.Context(), action, in.IDs)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, res)
}

func (h *CampaignHandler) Preflight(c *gin.Context) {
	var in domain.CreateCampaignInput
	if err := c.ShouldBindJSON(&in); err != nil {
		response.Fail(c, errcode.InvalidParam)
		return
	}
	res, err := h.svc.Preflight(c.Request.Context(), in)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, res)
}

func (h *CampaignHandler) EstimateAudience(c *gin.Context) {
	var in domain.AudienceEstimateInput
	if err := c.ShouldBindJSON(&in); err != nil {
		response.Fail(c, errcode.InvalidParam)
		return
	}
	res, err := h.svc.EstimateAudience(c.Request.Context(), in)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, res)
}

func (h *CampaignHandler) DryRun(c *gin.Context) {
	var in domain.DryRunInput
	if err := c.ShouldBindJSON(&in); err != nil {
		response.Fail(c, errcode.InvalidParam)
		return
	}
	res, err := h.svc.DryRun(c.Request.Context(), in)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, res)
}

func (h *CampaignHandler) Copy(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		response.Fail(c, errcode.InvalidParam)
		return
	}
	var in domain.CopyCampaignInput
	if err := c.ShouldBindJSON(&in); err != nil {
		response.Fail(c, errcode.InvalidParam)
		return
	}
	res, err := h.svc.Copy(c.Request.Context(), id, in)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, res)
}

func (h *CampaignHandler) Publish(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		response.Fail(c, errcode.InvalidParam)
		return
	}
	res, err := h.svc.Publish(c.Request.Context(), id)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, res)
}

func (h *CampaignHandler) UpdateDraft(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		response.Fail(c, errcode.InvalidParam)
		return
	}
	var in domain.CreateCampaignInput
	if err := c.ShouldBindJSON(&in); err != nil {
		response.Fail(c, errcode.InvalidParam)
		return
	}
	res, err := h.svc.UpdateDraft(c.Request.Context(), id, in)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, res)
}

func (h *CampaignHandler) Funnel(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		response.Fail(c, errcode.InvalidParam)
		return
	}
	res, err := h.svc.Funnel(c.Request.Context(), id)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, res)
}

func (h *CampaignHandler) Failures(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		response.Fail(c, errcode.InvalidParam)
		return
	}
	res, err := h.svc.FailureAnalysis(c.Request.Context(), id)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, res)
}

func (h *CampaignHandler) ListRecords(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		response.Fail(c, errcode.InvalidParam)
		return
	}
	var q domain.ListPushRecordQuery
	_ = c.ShouldBindQuery(&q)
	res, err := h.svc.ListRecords(c.Request.Context(), id, q)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, res)
}

func (h *CampaignHandler) ExportSync(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		response.Fail(c, errcode.InvalidParam)
		return
	}
	kind := c.DefaultQuery("kind", "records")
	data, filename, err := h.svc.SyncExportCSV(c.Request.Context(), id, kind)
	if err != nil {
		response.Fail(c, err)
		return
	}
	c.Header("Content-Disposition", "attachment; filename="+filename)
	c.Data(http.StatusOK, "text/csv; charset=utf-8", data)
}

func (h *CampaignHandler) ExportAsync(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		response.Fail(c, errcode.InvalidParam)
		return
	}
	var body struct {
		Kind      string `json:"kind"`
		CreatedBy string `json:"created_by"`
	}
	_ = c.ShouldBindJSON(&body)
	job, err := h.svc.CreateExport(c.Request.Context(), id, body.Kind, body.CreatedBy)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, job)
}

func (h *CampaignHandler) GetExport(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		response.Fail(c, errcode.InvalidParam)
		return
	}
	job, err := h.svc.GetExport(c.Request.Context(), id)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, job)
}

func (h *CampaignHandler) DownloadExport(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		response.Fail(c, errcode.InvalidParam)
		return
	}
	job, err := h.svc.GetExport(c.Request.Context(), id)
	if err != nil {
		response.Fail(c, err)
		return
	}
	if job.Status != domain.ExportStatusSuccess || job.FilePath == "" {
		response.Fail(c, errcode.InvalidParam)
		return
	}
	if _, err := os.Stat(job.FilePath); err != nil {
		response.Fail(c, errcode.NotFound)
		return
	}
	c.FileAttachment(job.FilePath, "export_"+strconv.FormatUint(id, 10)+".csv")
}
