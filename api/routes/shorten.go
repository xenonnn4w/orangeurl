package routes

import (
	"os"
	"strconv"
	"time"

	"github.com/asaskevich/govalidator"
	"github.com/go-redis/redis/v8"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/xenonnn4w/short-url/database"
	"github.com/xenonnn4w/short-url/helpers"
)

type request struct {
	URL         string        `json:"url"`
	CustomShort string        `json:"short"`
	Expiry      time.Duration `json:"expiry"`
}

type response struct {
	URL             string        `json:"url"`
	CustomShort     string        `json:"short"`
	Expiry          time.Duration `json:"expiry"`
	XRateRemaining  int           `json:"rate_left"`
	XRateLimitReset time.Duration `json:"rate_limit_reset"`
}

func ShortenURL(c *fiber.Ctx) error {
	body := new(request)

	if err := c.BodyParser(&body); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "cannot parse json"})
	}

	// rate limiting
	r2 := database.CreateClient(1)
	defer r2.Close()

	// determine effective quota once (fallback to 10 if unset)
	quota := os.Getenv("API_QUOTA")
	if quota == "" {
		quota = "10"
	}

	val, err := r2.Get(database.Ctx, c.IP()).Result()
	if err == redis.Nil {
		// initialize quota for this IP
		r2.Set(database.Ctx, c.IP(), quota, 30*60*time.Second).Err()
	} else {
		valInt, convErr := strconv.Atoi(val)
		if convErr != nil {
			// repair bad value by resetting quota
			r2.Set(database.Ctx, c.IP(), quota, 30*60*time.Second).Err()
		} else if valInt <= 0 {
			limit, _ := r2.TTL(database.Ctx, c.IP()).Result()
			return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{
				"error": "Rate limit exceeded",
				//doubt
				"rate_limit_rest": limit / time.Nanosecond / time.Minute,
			})
		}
	}

	// checking if the url is an actual url
	if !govalidator.IsURL(body.URL) {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid URL"})
	}

	// checking for domain error
	if !helpers.RemoveDomainError(body.URL) {
		return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{"error": "Chodu ho kya"})
	}

	// enforce https

	body.URL = helpers.EnforceHTTP(body.URL)

	// assigning url according to the custom id
	var id string

	if body.CustomShort == "" {
		id = uuid.New().String()[:6]
	} else {
		id = body.CustomShort
	}

	r := database.CreateClient(0)
	defer r.Close()

	val, _ = r.Get(database.Ctx, id).Result()
	if val != "" {
		return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "URL already taken"})
	}

	// checking the expiry

	if body.Expiry == 0 {
		body.Expiry = 24
	}

	//doubt regarding the time
	err = r.Set(database.Ctx, id, body.URL, body.Expiry*3600*time.Second).Err()

	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "unable to connect to server"})
	}

	resp := response{
		URL:             body.URL,
		CustomShort:     "",
		Expiry:          body.Expiry,
		XRateRemaining:  10,
		XRateLimitReset: 30,
	}

	// decremented the rateremeaning
	r2.Decr(database.Ctx, c.IP())

	val, _ = r2.Get(database.Ctx, c.IP()).Result()
	resp.XRateRemaining, _ = strconv.Atoi(val)

	// time doubt
	ttl, _ := r2.TTL(database.Ctx, c.IP()).Result()
	resp.XRateLimitReset = ttl / time.Nanosecond / time.Minute

	resp.CustomShort = os.Getenv("DOMAIN") + "/" + id

	return c.Status(fiber.StatusOK).JSON(resp)
}
