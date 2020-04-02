package controllers

import (
	"github.com/faeelol/multi-tenant-service/database"
	"github.com/faeelol/multi-tenant-service/models"
	"github.com/gin-gonic/gin"
	"github.com/jinzhu/gorm"
	uuid "github.com/satori/go.uuid"
	"net/http"
	"strings"
)

type TenantsController struct {
	ControllerBase
}

func (t TenantsController) CreateTenant(c *gin.Context) {
	authUser := GetAuthUserClaims(c)
	if authUser.Role != models.TAdmin {
		t.JsonFail(c, http.StatusForbidden, "Access is denied")
		return
	}

	var newTenant models.TenantPost
	if err := c.Bind(&newTenant); err != nil {
		t.JsonFail(c, http.StatusBadRequest, err.Error())
		return
	}

	authTenantId, err := uuid.FromString(authUser.TenantId)
	if err != nil {
		t.JsonFail(c, http.StatusConflict, "invalid authorized tenant")
		return
	}
	parentId, err := uuid.FromString(newTenant.ParentId)
	if err != nil {
		t.JsonFail(c, http.StatusBadRequest, "invalid parent_id format")
		return
	}
	if !isChildAvailable(authTenantId, parentId) {
		t.JsonFail(c, http.StatusForbidden, "access is denied")
		return
	}
	if !isTenantNameFree(newTenant.Name, parentId) {
		t.JsonFail(c, http.StatusBadRequest, "name is already taken")
		return
	}

	newTenantId, err := uuid.NewV4()
	if err != nil {
		panic(err)
	}

	createTenant := models.Tenant{
		ID:              &newTenantId,
		Name:            newTenant.Name,
		ParentId:        &parentId,
		OwnerId:         &authTenantId,
		AncestralAccess: true,
		Version:         1,
	}
	if err := database.DB.Create(&createTenant).Error; err != nil {
		panic(err)
	}
	t.JsonSuccess(c, http.StatusCreated, createTenant.ToBasicTenantSchema())
}

func (t TenantsController) FetchTenantsBatch(c *gin.Context) {
	authUser := GetAuthUserClaims(c)
	authTenantId, err := uuid.FromString(authUser.TenantId)
	if err != nil {
		t.JsonFail(c, http.StatusConflict, "invalid authorized tenant")
		return
	}
	var tenants []models.Tenant
	var uuids []uuid.UUID
	ids := c.Request.URL.Query().Get("uuids")
	tenant := c.Request.URL.Query().Get("tenant_id")
	if ids != "" {
		for _, id := range strings.Split(ids, ",") {
			if cur, err := uuid.FromString(id); err == nil {
				uuids = append(uuids, cur)
			}
		}
		if err := database.DB.Where("id IN (?)", uuids).Find(&tenants).Error; err != nil {
			panic(err)
		}
	} else if tenant != "" {
		parentId, err := uuid.FromString(tenant)
		if err != nil {
			t.JsonFail(c, http.StatusBadRequest, "invalid parent_id format")
			return
		}
		if err := database.DB.Where("parent_id = ?", parentId).Find(&tenants).Error; err != nil {
			panic(err)
		}
	} else {
		t.JsonFail(c, http.StatusBadRequest, "specify `tenant_id` or `uuids` in query")
		return
	}
	var results models.TenantsBatch
	results.Items = make([]models.BasicTenantSchema, 0)
	for _, tenant := range tenants {
		if isChildAvailable(authTenantId, *tenant.ID) {
			results.Items = append(results.Items, tenant.ToBasicTenantSchema())
		}
	}
	t.JsonSuccess(c, http.StatusOK, results)
}

func isTenantNameFree(name string, parentId uuid.UUID) bool {
	var user models.Tenant
	return gorm.IsRecordNotFoundError(
		database.DB.Where("name = ? AND parent_id = ?", name, parentId).First(&user).Error)
}

func isChildAvailable(parent uuid.UUID, child uuid.UUID) bool {
	if parent == child {
		return true
	}
	current := child
	for true {
		var tenant models.Tenant
		if err := database.DB.Where("id = ?", current).First(&tenant).Error; err != nil {
			return false
		}
		if !tenant.AncestralAccess {
			return false
		}
		if *tenant.ParentId == parent {
			return true
		}
		if *tenant.ParentId == *tenant.ID {
			return false
		}
		current = *tenant.ParentId
	}
	return false
}
