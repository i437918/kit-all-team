package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"

	"github.com/mi1man-cmd/kit-all-team/internal/domain"
	"github.com/mi1man-cmd/kit-all-team/internal/reconcile"
)

func withMutationProgress(ctx context.Context, writer io.Writer, enabled bool) context.Context {
	if !enabled || writer == nil {
		return ctx
	}
	header, writable := false, true
	return reconcile.WithProgressObserver(ctx, func(event reconcile.ProgressEvent) {
		if !writable {
			return
		}
		line := progressLine(event)
		if line == "" {
			return
		}
		if !header {
			if _, err := fmt.Fprintln(writer, "Обработка данных ... подождите"); err != nil {
				writable = false
				return
			}
			header = true
		}
		if _, err := fmt.Fprintln(writer, line); err != nil {
			writable = false
		}
	})
}

func withJSONEventProgress(ctx context.Context, writer io.Writer) context.Context {
	if writer == nil {
		return ctx
	}
	writable := true
	return reconcile.WithProgressObserver(ctx, func(event reconcile.ProgressEvent) {
		if !writable {
			return
		}
		phase := wizardProgressPhase(event)
		if phase == "" {
			return
		}
		if err := json.NewEncoder(writer).Encode(wizardEvent{SchemaVersion: 1, Event: "progress", Phase: phase}); err != nil {
			writable = false
		}
	})
}

func wizardProgressPhase(event reconcile.ProgressEvent) string {
	target := string(event.Action)
	if target == "" {
		target = string(event.Target)
	}
	if target == "" || event.Phase == "" {
		return ""
	}
	return target + ":" + string(event.Phase)
}

func progressLine(event reconcile.ProgressEvent) string {
	if event.Target == reconcile.ProgressHermesCredentials {
		return progressStatus("Учетные данные Hermes (.env)", "сохранение", event.Phase)
	}
	if event.Target == reconcile.ProgressHermesHome {
		return progressStatus("Переменная HERMES_HOME для текущего пользователя", "сохранение", event.Phase)
	}
	var noun, activity string
	switch event.Action {
	case reconcile.ActionPrepareWorkspace:
		noun, activity = "Каталог проекта", "подготовка"
	case reconcile.ActionSyncContent:
		noun, activity = "Шаблоны Team Kit", "копирование из GitLab"
	case reconcile.ActionSyncDatabase:
		noun, activity = "База данных проекта", "копирование из GitLab"
	case reconcile.ActionInstallToolchain:
		noun, activity = "Набор skills", "установка"
	case reconcile.ActionConfigureApplication:
		if event.Application == string(domain.AppHermes) {
			noun, activity = "Hermes: профиль, провайдер, модель и MCP-серверы", "настройка"
		} else {
			noun, activity = "AI-приложение", "настройка"
		}
	case reconcile.ActionVerifyState:
		noun, activity = "Окружение проекта", "финальная проверка"
	default:
		return ""
	}
	return progressStatus(noun, activity, event.Phase)
}

func directProgress(writer io.Writer, enabled bool) func(reconcile.ProgressEvent) {
	writable := enabled && writer != nil
	return func(event reconcile.ProgressEvent) {
		if !writable {
			return
		}
		line := progressLine(event)
		if line == "" {
			return
		}
		func() {
			defer func() {
				if recover() != nil {
					writable = false
				}
			}()
			if _, err := fmt.Fprintln(writer, line); err != nil {
				writable = false
			}
		}()
	}
}

func progressStatus(noun, started string, phase reconcile.ProgressPhase) string {
	switch phase {
	case reconcile.ProgressStarted:
		return noun + " .. " + started
	case reconcile.ProgressCompleted:
		return noun + " .. готово"
	case reconcile.ProgressFailed:
		return noun + " .. ошибка"
	default:
		return ""
	}
}
