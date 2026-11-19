package model

import "fmt"

// reason templates: [english, arabic]
var msgs = map[string][2]string{
	"after":       {"you usually run this after `%s`", "بتشغّله عادةً بعد `%s`"},
	"after2":      {"your usual sequence after `%s`", "تسلسل متكرر عندك بعد `%s`"},
	"afterProg":   {"often follows `%s` commands", "بييجي غالبًا بعد أوامر `%s`"},
	"here":        {"you often run this in this folder", "بتشغّله كتير في الفولدر ده"},
	"history":     {"from your history (%d×)", "من سجلّك (%d مرة)"},
	"popular":     {"one of your most used commands", "من أكتر أوامرك استخدامًا"},
	"fileHere":    {"file in this folder", "ملف في الفولدر الحالي"},
	"dirHere":     {"folder in this folder", "فولدر جوه الفولدر الحالي"},
	"foundIn":     {"found in %s", "لقيته في %s"},
	"recentFile":  {"recently modified here", "اتعدّل قريب هنا"},
	"jump":        {"frequent folder (%s)", "فولدر بتروحه كتير (%s)"},
	"copyTo":      {"copy to where you usually do: %s", "نسخ للمكان اللي بتنسخ له عادةً: %s"},
	"dest":        {"where you usually put files", "المكان اللي بتحط فيه ملفاتك عادةً"},
	"typo":        {"`%s` isn't a command — did you mean `%s`?", "`%s` مش أمر — قصدك `%s`؟"},
	"notFound":    {"last command wasn't found — fixed typo", "الأمر اللي فات مش موجود — صلّحت الغلطة"},
	"sudo":        {"previous command failed — retry with sudo", "الأمر اللي فات فشل — جرّبه بـ sudo"},
	"gitDirty":    {"git: %d changed file(s)", "git: فيه %d ملف متغيّر"},
	"gitStaged":   {"git: %d staged file(s) ready to commit", "git: فيه %d ملف جاهز للـ commit"},
	"gitAhead":    {"git: %d commit(s) not pushed yet", "git: فيه %d commit لسه ما اترفعش"},
	"gitBehind":   {"git: remote is %d commit(s) ahead", "git: الريموت سابقك بـ %d commit"},
	"gitBranch":   {"publish the new branch", "ارفع البرانش الجديد"},
	"cloned":      {"enter the repo you just cloned", "ادخل الريبو اللي لسه عملتله clone"},
	"mkdir":       {"enter the folder you just created", "ادخل الفولدر اللي لسه عملته"},
	"extracted":   {"enter the extracted folder", "ادخل الفولدر اللي اتفك"},
	"extract":     {"extract what you just downloaded", "فك الملف اللي لسه نزّلته"},
	"project":     {"%s project detected", "مشروع %s"},
	"projectDeps": {"%s project — dependencies not installed yet", "مشروع %s — الـ dependencies لسه ما اتسطبتش"},
	"script":      {"%s script: `%s`", "سكربت %s: `%s`"},
	"thenRun":     {"next step after `%s`", "الخطوة الطبيعية بعد `%s`"},
	"activate":    {"activate the virtualenv you just made", "فعّل الـ virtualenv اللي لسه عملته"},
	"runEdited":   {"run the file you just edited", "شغّل الملف اللي لسه عدّلته"},
	"runChmod":    {"run the file you just made executable", "شغّل الملف اللي لسه خليته executable"},
	"pubkey":      {"show your new public key", "اعرض المفتاح العام الجديد"},
	"status":      {"check the service you just touched", "شوف حالة السيرفس اللي لسه لمسته"},
}

func T(lang, key string, args ...any) string {
	m, ok := msgs[key]
	if !ok {
		return key
	}
	f := m[0]
	if lang == "ar" {
		f = m[1]
	}
	if len(args) == 0 {
		return f
	}
	return fmt.Sprintf(f, args...)
}
