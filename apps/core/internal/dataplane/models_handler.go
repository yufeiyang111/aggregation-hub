package dataplane

import (
	"context"
	"encoding/json"
	"net/http"

	"aggregationhub.local/core/internal/provider"
)

type publicModelReader interface {
	ListPublic(context.Context) ([]provider.PublicModel, error)
}

type openAIModelDTO struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	Owner   string `json:"owned_by"`
	Created int64  `json:"created"`
}

type openAIModelListResponse struct {
	Object string           `json:"object"`
	Data   []openAIModelDTO `json:"data"`
}

type openAIModelsErrorResponse struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func NewModelsHandler(reader publicModelReader) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if reader == nil {
			writeModelsError(writer)
			return
		}
		models, err := reader.ListPublic(request.Context())
		if err != nil {
			writeModelsError(writer)
			return
		}
		data := make([]openAIModelDTO, 0, len(models))
		for _, model := range models {
			data = append(data, openAIModelDTO{ID: model.ID, Object: "model", Owner: model.Owner, Created: 0})
		}
		writer.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(writer).Encode(openAIModelListResponse{Object: "list", Data: data})
	})
}

func writeModelsError(writer http.ResponseWriter) {
	response := openAIModelsErrorResponse{}
	response.Error.Code = "models_unavailable"
	response.Error.Message = "模型列表暂不可用"
	writer.Header().Set("Content-Type", "application/json; charset=utf-8")
	writer.WriteHeader(http.StatusInternalServerError)
	_ = json.NewEncoder(writer).Encode(response)
}
