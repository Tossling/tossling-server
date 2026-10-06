package main

import (
	"fmt"
	"net/http"
	"strings"
	"time"
)

type Texts map[string]string

var english = Texts{
	"Lang":          "en",
	"Running":       "The server is running",
	"Lead":          "One clipboard for your Macs and phone.",
	"Note":          "This server only relays encrypted messages between the devices of a room.",
	"Project":       "Tossling on GitHub",
	"SetupTitle":    "The server is ready",
	"SetupSubtitle": "Now connect a Mac, it takes about a minute",
	"Address":       "Address for the devices",
	"NoHTTPS":       "No HTTPS: the token and files travel unprotected. Put the server behind an HTTPS proxy.",
	"OtherHost":     "This page is open at %s, but the devices are told %s. Check TOSSLING_BASE_URL.",
	"Files":         "Files up to 500 MB, kept for 3 hours",
	"StepInstall":   "Install Tossling on the Mac",
	"InstallNote":   "The steps are in the Tossling repository",
	"StepRun":       "On the Mac, run",
	"StepToken":     "When it asks for the token, paste",
	"TokenNote":     "Anyone with this token can use the server. Do not share it.",
	"StepPhone":     "The Mac shows a QR code: scan it in Tossling on the phone",
	"Copy":          "Copy",
	"Copied":        "Copied",
	"Done":          "Done, close this page",
	"DoneNote":      "The link works until you press Done. A new one:",
	"LostTitle":     "This link does not work",
	"LostText":      "It is wrong or already used. A new one on the server:",

	"AdminTitle":       "Panel password",
	"AdminNote":        "To manage projects in the browser",
	"AdminSet":         "The password is set",
	"Password":         "Password",
	"PasswordRepeat":   "Repeat the password",
	"SavePassword":     "Save the password",
	"PasswordShort":    "The password must be at least 10 characters",
	"PasswordMismatch": "The passwords do not match",
	"OpenPanel":        "Open the panel",
	"LoginTitle":       "Tossling Server",
	"LoginSubtitle":    "Sign in to the panel",
	"LoginButton":      "Sign in",
	"LoginWrong":       "Wrong password",
	"LoginLimited":     "Too many attempts. Try again in 15 minutes.",
	"NoAdmin":          "No panel password yet",
	"NoAdminText":      "Set it on the setup page or on the server with",
	"NavOverview":      "Overview",
	"NavProjects":      "Projects",
	"NavSecurity":      "Security",
	"Logout":           "Sign out",
	"Server":           "Server",
	"Version":          "Version",
	"Firebase":         "Instant delivery on Android",
	"FirebaseOn":       "on",
	"FirebaseOff":      "off: no Firebase key",
	"Storage":          "Storage",
	"Attachments":      "Attachments",
	"Cache":            "Message cache",
	"Activity":         "Activity",
	"Rooms":            "Rooms with activity",
	"RoomsNote":        "Room contents are encrypted on the devices; the server sees only that they are used.",
	"LastActivity":     "Last activity",
	"EventsDay":        "Project events today",
	"ProjectsCount":    "Projects",
	"PublishersCount":  "Publishers",
	"ProjectsTitle":    "Projects",
	"ProjectsNote":     "Channels services publish events to; every Tossling device shows them.",
	"NoProjects":       "No projects yet",
	"NewProject":       "New project",
	"Channel":          "Channel",
	"ChannelHint":      "Latin letters, digits, - and _",
	"Name":             "Name",
	"Publisher":        "Publisher",
	"PublisherHint":    "Optional: an existing publisher keeps its token",
	"Create":           "Create",
	"DevicesWrite":     "written by the devices",
	"NoPublisher":      "no publisher",
	"PerDay":           "today",
	"Events":           "Events",
	"NoEvents":         "No events yet",
	"Live":             "New events appear here as they arrive",
	"Rename":           "Name",
	"Save":             "Save",
	"SendTest":         "Send a test event",
	"TestTitle":        "Test",
	"TestMessage":      "A test event from the Tossling Server panel",
	"TestSent":         "The test event is sent",
	"NewToken":         "New publisher token",
	"NewTokenNote":     "The old token of %s stops working in all its channels.",
	"Delete":           "Delete the project",
	"DeleteNote":       "Services can no longer publish here and the devices stop showing the channel.",
	"Example":          "Request example",
	"TokenOnce":        "The token is shown once: put it into the service settings now.",
	"TokenTitle":       "Publisher token",
	"Back":             "Back",
	"Publishers":       "Publishers",
	"Channels":         "Channels",
	"LastUsed":         "Last used",
	"NeverUsed":        "not used yet",
	"NoPublishers":     "No publishers yet",
	"SetupLink":        "New setup link",
	"SetupLinkNote":    "Opens the page with the command and the device token. Works until Done is pressed there.",
	"SetupLinkReady":   "The link works until Done is pressed on its page:",
	"ChangePassword":   "Change the password",
	"CurrentPassword":  "Current password",
	"NewPassword":      "New password",
	"PasswordChanged":  "The password is changed",
	"LogoutAll":        "Sign out everywhere",
	"LogoutAllNote":    "Every open panel session ends.",
	"Wrong":            "Something went wrong",
}

var russian = Texts{
	"Lang":          "ru",
	"Running":       "Сервер работает",
	"Lead":          "Общий буфер обмена твоих Mac и телефона.",
	"Note":          "Этот сервер только пересылает зашифрованные сообщения между устройствами комнаты.",
	"Project":       "Tossling на GitHub",
	"SetupTitle":    "Сервер готов",
	"SetupSubtitle": "Осталось подключить Mac — около минуты",
	"Address":       "Адрес для устройств",
	"NoHTTPS":       "Без HTTPS токен и файлы идут открыто. Поставь сервер за HTTPS-прокси.",
	"OtherHost":     "Страница открыта по адресу %s, а устройствам сервер сообщает %s. Проверь TOSSLING_BASE_URL.",
	"Files":         "Файлы до 500 МБ, хранятся 3 часа",
	"StepInstall":   "Поставь Tossling на Mac",
	"InstallNote":   "Инструкция — в репозитории Tossling",
	"StepRun":       "На Mac выполни",
	"StepToken":     "Когда спросит токен, вставь",
	"TokenNote":     "С этим токеном к серверу подключится кто угодно — не публикуй его.",
	"StepPhone":     "Mac покажет QR-код — отсканируй его в Tossling на телефоне",
	"Copy":          "Скопировать",
	"Copied":        "Скопировано",
	"Done":          "Готово, закрыть страницу",
	"DoneNote":      "Ссылка работает, пока не нажмёшь «Готово». Новую выдаст команда",
	"LostTitle":     "Ссылка не работает",
	"LostText":      "Она неверна или уже использована. Новую выдаст на сервере команда",

	"AdminTitle":       "Пароль для панели",
	"AdminNote":        "Чтобы управлять проектами в браузере",
	"AdminSet":         "Пароль задан",
	"Password":         "Пароль",
	"PasswordRepeat":   "Повтори пароль",
	"SavePassword":     "Сохранить пароль",
	"PasswordShort":    "Пароль — не короче 10 символов",
	"PasswordMismatch": "Пароли не совпадают",
	"OpenPanel":        "Открыть панель",
	"LoginTitle":       "Tossling Server",
	"LoginSubtitle":    "Вход в панель",
	"LoginButton":      "Войти",
	"LoginWrong":       "Неверный пароль",
	"LoginLimited":     "Слишком много попыток. Попробуй через 15 минут.",
	"NoAdmin":          "Пароль панели ещё не задан",
	"NoAdminText":      "Задай его на странице первого запуска или на сервере командой",
	"NavOverview":      "Обзор",
	"NavProjects":      "Проекты",
	"NavSecurity":      "Безопасность",
	"Logout":           "Выйти",
	"Server":           "Сервер",
	"Version":          "Версия",
	"Firebase":         "Мгновенная доставка на Android",
	"FirebaseOn":       "включена",
	"FirebaseOff":      "выключена: нет ключа Firebase",
	"Storage":          "Хранилище",
	"Attachments":      "Вложения",
	"Cache":            "Кеш сообщений",
	"Activity":         "Активность",
	"Rooms":            "Комнат с активностью",
	"RoomsNote":        "Содержимое комнат зашифровано на устройствах — сервер видит только, что ими пользуются.",
	"LastActivity":     "Последняя активность",
	"EventsDay":        "Событий проектов за сутки",
	"ProjectsCount":    "Проектов",
	"PublishersCount":  "Отправителей",
	"ProjectsTitle":    "Проекты",
	"ProjectsNote":     "Каналы, в которые сервисы шлют события; их показывают все устройства Tossling.",
	"NoProjects":       "Проектов пока нет",
	"NewProject":       "Новый проект",
	"Channel":          "Канал",
	"ChannelHint":      "Латиница, цифры, - и _",
	"Name":             "Название",
	"Publisher":        "Отправитель",
	"PublisherHint":    "Необязательно: существующий отправитель останется со своим токеном",
	"Create":           "Создать",
	"DevicesWrite":     "пишут устройства",
	"NoPublisher":      "нет отправителя",
	"PerDay":           "за сутки",
	"Events":           "События",
	"NoEvents":         "Событий пока нет",
	"Live":             "Новые события появляются здесь сразу",
	"Rename":           "Название",
	"Save":             "Сохранить",
	"SendTest":         "Отправить тестовое",
	"TestTitle":        "Проверка",
	"TestMessage":      "Тестовое событие из панели Tossling Server",
	"TestSent":         "Тестовое событие отправлено",
	"NewToken":         "Новый токен отправителя",
	"NewTokenNote":     "Старый токен %s перестанет работать во всех его каналах.",
	"Delete":           "Удалить проект",
	"DeleteNote":       "Сервисы больше не смогут писать сюда, а устройства перестанут показывать канал.",
	"Example":          "Пример запроса",
	"TokenOnce":        "Токен показывается один раз — сразу пропиши его в настройках сервиса.",
	"TokenTitle":       "Токен отправителя",
	"Back":             "Назад",
	"Publishers":       "Отправители",
	"Channels":         "Каналы",
	"LastUsed":         "Последний раз",
	"NeverUsed":        "ещё не использовался",
	"NoPublishers":     "Отправителей пока нет",
	"SetupLink":        "Новая ссылка настройки",
	"SetupLinkNote":    "Откроет страницу с командой и токеном устройств. Работает, пока там не нажмут «Готово».",
	"SetupLinkReady":   "Ссылка работает, пока на её странице не нажмут «Готово»:",
	"ChangePassword":   "Сменить пароль",
	"CurrentPassword":  "Текущий пароль",
	"NewPassword":      "Новый пароль",
	"PasswordChanged":  "Пароль изменён",
	"LogoutAll":        "Выйти на всех устройствах",
	"LogoutAllNote":    "Все открытые сессии панели закончатся.",
	"Wrong":            "Что-то пошло не так",
}

func textsFor(r *http.Request) Texts {
	for _, part := range strings.Split(r.Header.Get("Accept-Language"), ",") {
		tag := strings.ToLower(strings.TrimSpace(strings.SplitN(part, ";", 2)[0]))
		switch {
		case strings.HasPrefix(tag, "ru"):
			return russian
		case strings.HasPrefix(tag, "en"):
			return english
		}
	}
	return english
}

func (t Texts) f(key string, args ...any) string {
	return fmt.Sprintf(t[key], args...)
}

func formatTime(t time.Time) string {
	if t.IsZero() || t.Unix() <= 0 {
		return "—"
	}
	if time.Since(t) < 24*time.Hour && t.Day() == time.Now().Day() {
		return t.Local().Format("15:04")
	}
	return t.Local().Format("02.01 15:04")
}

func formatSize(n int64) string {
	switch {
	case n >= 1<<30:
		return fmt.Sprintf("%.1f GB", float64(n)/(1<<30))
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%d KB", n>>10)
	}
	return fmt.Sprintf("%d B", n)
}
