package cli

import (
	"testing"

	"github.com/mi1man-cmd/kit-all-team/internal/domain"
	"github.com/mi1man-cmd/kit-all-team/internal/reconcile"
)

func TestProgressLine_ExactRussianStringsForEveryTargetAndPhase(t *testing.T) {
	tests := []struct {
		name      string
		event     reconcile.ProgressEvent
		started   string
		completed string
		failed    string
	}{
		{"credentials", reconcile.ProgressEvent{Target: reconcile.ProgressHermesCredentials}, "Учетные данные Hermes (.env) .. сохранение", "Учетные данные Hermes (.env) .. готово", "Учетные данные Hermes (.env) .. ошибка"},
		{"workspace", reconcile.ProgressEvent{Target: reconcile.ProgressAction, Action: reconcile.ActionPrepareWorkspace}, "Каталог проекта .. подготовка", "Каталог проекта .. готово", "Каталог проекта .. ошибка"},
		{"content", reconcile.ProgressEvent{Target: reconcile.ProgressAction, Action: reconcile.ActionSyncContent}, "Шаблоны Team Kit .. копирование из GitLab", "Шаблоны Team Kit .. готово", "Шаблоны Team Kit .. ошибка"},
		{"database", reconcile.ProgressEvent{Target: reconcile.ProgressAction, Action: reconcile.ActionSyncDatabase}, "База данных проекта .. копирование из GitLab", "База данных проекта .. готово", "База данных проекта .. ошибка"},
		{"toolchain", reconcile.ProgressEvent{Target: reconcile.ProgressAction, Action: reconcile.ActionInstallToolchain}, "Набор skills .. установка", "Набор skills .. готово", "Набор skills .. ошибка"},
		{"Hermes", reconcile.ProgressEvent{Target: reconcile.ProgressAction, Action: reconcile.ActionConfigureApplication, Application: string(domain.AppHermes)}, "Hermes: профиль, провайдер, модель и MCP-серверы .. настройка", "Hermes: профиль, провайдер, модель и MCP-серверы .. готово", "Hermes: профиль, провайдер, модель и MCP-серверы .. ошибка"},
		{"other app", reconcile.ProgressEvent{Target: reconcile.ProgressAction, Action: reconcile.ActionConfigureApplication, Application: string(domain.AppCodex)}, "AI-приложение .. настройка", "AI-приложение .. готово", "AI-приложение .. ошибка"},
		{"verify", reconcile.ProgressEvent{Target: reconcile.ProgressAction, Action: reconcile.ActionVerifyState}, "Окружение проекта .. финальная проверка", "Окружение проекта .. готово", "Окружение проекта .. ошибка"},
		{"HERMES_HOME", reconcile.ProgressEvent{Target: reconcile.ProgressHermesHome}, "Переменная HERMES_HOME для текущего пользователя .. сохранение", "Переменная HERMES_HOME для текущего пользователя .. готово", "Переменная HERMES_HOME для текущего пользователя .. ошибка"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			for _, phase := range []struct {
				value reconcile.ProgressPhase
				want  string
			}{
				{reconcile.ProgressStarted, test.started},
				{reconcile.ProgressCompleted, test.completed},
				{reconcile.ProgressFailed, test.failed},
			} {
				event := test.event
				event.Phase = phase.value
				if got := progressLine(event); got != phase.want {
					t.Fatalf("progressLine(%+v)=%q want=%q", event, got, phase.want)
				}
			}
		})
	}
}
