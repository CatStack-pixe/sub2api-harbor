package service

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/textproto"
	"strings"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
)

// Task type constants
const (
	TaskTypeVerifyCode    = "verify_code"
	TaskTypePasswordReset = "password_reset"
)

// EmailTask 邮件发送任务
type EmailTask struct {
	Email    string
	SiteName string
	TaskType string // "verify_code" or "password_reset"
	ResetURL string // Only used for password_reset task type
	Locale   string // Optional Accept-Language locale hint
	Prepared *PreparedVerifyCode
}

// EmailQueueService 异步邮件队列服务
type EmailQueueService struct {
	emailService *EmailService
	taskChan     chan EmailTask
	wg           sync.WaitGroup
	stopChan     chan struct{}
	workers      int
	stateMu      sync.RWMutex
	stopped      bool
}

// NewEmailQueueService 创建邮件队列服务
func NewEmailQueueService(emailService *EmailService, workers int) *EmailQueueService {
	if workers <= 0 {
		workers = 3 // 默认3个工作协程
	}

	service := &EmailQueueService{
		emailService: emailService,
		taskChan:     make(chan EmailTask, 100), // 缓冲100个任务
		stopChan:     make(chan struct{}),
		workers:      workers,
	}

	// 启动工作协程
	service.start()

	return service
}

// start 启动工作协程
func (s *EmailQueueService) start() {
	for i := 0; i < s.workers; i++ {
		s.wg.Add(1)
		go s.worker(i)
	}
	logger.LegacyPrintf("service.email_queue", "[EmailQueue] Started %d workers", s.workers)
}

// worker 工作协程
func (s *EmailQueueService) worker(id int) {
	defer s.wg.Done()

	for {
		select {
		case task := <-s.taskChan:
			s.processTask(id, task)
		case <-s.stopChan:
			for {
				select {
				case task := <-s.taskChan:
					s.processTask(id, task)
				default:
					logger.LegacyPrintf("service.email_queue", "[EmailQueue] Worker %d stopping", id)
					return
				}
			}
		}
	}
}

// processTask 处理任务
func (s *EmailQueueService) processTask(workerID int, task EmailTask) {
	ctx, cancel := context.WithTimeout(context.Background(), 75*time.Second)
	defer cancel()

	switch task.TaskType {
	case TaskTypeVerifyCode:
		if task.Prepared == nil {
			logger.LegacyPrintf("service.email_queue", "[EmailQueue] Worker %d rejected unprepared verify task", workerID)
			return
		}
		if err := s.sendVerifyCodeWithRetry(ctx, task); err != nil {
			s.emailService.ReleaseVerifyCodeReservation(context.Background(), task.Email, task.Prepared.ReservationID)
			logger.LegacyPrintf("service.email_queue", "[EmailQueue] Worker %d failed to send verify code recipient_hash=%s: %v", workerID, notificationEmailHash(task.Email), err)
		} else {
			logger.LegacyPrintf("service.email_queue", "[EmailQueue] Worker %d sent verify code recipient_hash=%s", workerID, notificationEmailHash(task.Email))
		}
	case TaskTypePasswordReset:
		if err := s.sendWithRetry(ctx, task.Email, func() error {
			return s.emailService.SendPasswordResetEmailWithCooldown(ctx, task.Email, task.SiteName, task.ResetURL, task.Locale)
		}); err != nil {
			logger.LegacyPrintf("service.email_queue", "[EmailQueue] Worker %d failed to send password reset recipient_hash=%s: %v", workerID, notificationEmailHash(task.Email), err)
		} else {
			logger.LegacyPrintf("service.email_queue", "[EmailQueue] Worker %d sent password reset recipient_hash=%s", workerID, notificationEmailHash(task.Email))
		}
	default:
		logger.LegacyPrintf("service.email_queue", "[EmailQueue] Worker %d unknown task type: %s", workerID, task.TaskType)
	}
}

func (s *EmailQueueService) sendVerifyCodeWithRetry(ctx context.Context, task EmailTask) error {
	return s.sendWithRetry(ctx, task.Email, func() error {
		return s.emailService.SendPreparedVerifyCode(ctx, task.Email, task.SiteName, task.Prepared, task.Locale)
	})
}

func (s *EmailQueueService) sendWithRetry(ctx context.Context, email string, send func() error) error {
	var lastErr error
	delays := []time.Duration{0, 500 * time.Millisecond, 2 * time.Second}
	for attempt, delay := range delays {
		if delay > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(delay):
			}
		}
		lastErr = send()
		if lastErr == nil {
			return nil
		}
		if attempt == len(delays)-1 || !isRetryableEmailSendError(lastErr) {
			break
		}
		logger.LegacyPrintf("service.email_queue", "[EmailQueue] Retrying email recipient_hash=%s attempt=%d error=%v", notificationEmailHash(email), attempt+2, lastErr)
	}
	return lastErr
}

func isRetryableEmailSendError(err error) bool {
	if err == nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	lower := strings.ToLower(err.Error())
	if strings.Contains(lower, "smtp auth") || strings.Contains(lower, "email service not configured") || strings.Contains(lower, "invalid smtp") {
		return false
	}
	if strings.Contains(lower, "write msg") || strings.Contains(lower, "close writer") {
		return false
	}
	var smtpErr *textproto.Error
	if errors.As(err, &smtpErr) {
		return smtpErr.Code >= 400 && smtpErr.Code < 500
	}
	var networkErr net.Error
	if errors.As(err, &networkErr) {
		return true
	}
	return strings.Contains(lower, "smtp dial") || strings.Contains(lower, "tls handshake") || strings.Contains(lower, "starttls") || strings.Contains(lower, "new smtp client")
}

// EnqueueVerifyCode 将验证码发送任务加入队列
func (s *EmailQueueService) EnqueueVerifyCode(email, siteName string, locale ...string) error {
	return s.EnqueueVerifyCodeContext(context.Background(), email, siteName, locale...)
}

func (s *EmailQueueService) EnqueueVerifyCodeContext(ctx context.Context, email, siteName string, locale ...string) error {
	s.stateMu.RLock()
	defer s.stateMu.RUnlock()
	if s.stopped {
		return errors.New("email queue is stopped")
	}
	var prepared *PreparedVerifyCode
	if s.emailService != nil {
		var err error
		prepared, err = s.emailService.PrepareVerifyCode(ctx, email)
		if err != nil {
			return err
		}
	}
	task := EmailTask{
		Email:    email,
		SiteName: siteName,
		TaskType: TaskTypeVerifyCode,
		Locale:   firstEmailLocale(locale),
		Prepared: prepared,
	}

	select {
	case s.taskChan <- task:
		logger.LegacyPrintf("service.email_queue", "[EmailQueue] Enqueued verify code recipient_hash=%s", notificationEmailHash(email))
		return nil
	default:
		if s.emailService != nil && prepared != nil {
			s.emailService.ReleaseVerifyCodeReservation(ctx, email, prepared.ReservationID)
		}
		return fmt.Errorf("email queue is full")
	}
}

// EnqueuePasswordReset 将密码重置邮件任务加入队列
func (s *EmailQueueService) EnqueuePasswordReset(email, siteName, resetURL string, locale ...string) error {
	s.stateMu.RLock()
	defer s.stateMu.RUnlock()
	if s.stopped {
		return errors.New("email queue is stopped")
	}
	task := EmailTask{
		Email:    email,
		SiteName: siteName,
		TaskType: TaskTypePasswordReset,
		ResetURL: resetURL,
		Locale:   firstEmailLocale(locale),
	}

	select {
	case s.taskChan <- task:
		logger.LegacyPrintf("service.email_queue", "[EmailQueue] Enqueued password reset recipient_hash=%s", notificationEmailHash(email))
		return nil
	default:
		return fmt.Errorf("email queue is full")
	}
}

// Stop 停止队列服务
func (s *EmailQueueService) Stop() {
	s.stateMu.Lock()
	if s.stopped {
		s.stateMu.Unlock()
		return
	}
	s.stopped = true
	close(s.stopChan)
	s.stateMu.Unlock()
	s.wg.Wait()
	logger.LegacyPrintf("service.email_queue", "%s", "[EmailQueue] All workers stopped")
}
