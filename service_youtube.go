/*
 * MumbleDJ
 * By Matthieu Grieger
 * service_youtube.go
 * Copyright (c) 2014, 2015 Matthieu Grieger (MIT License)
 */

package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"github.com/jmoiron/jsonq"
)

// ------------
// YOUTUBE SONG
// ------------

// YouTubeSong holds the metadata for a song extracted from a YouTube video.
type YouTubeSong struct {
	submitter string
	title     string
	id        string
	filename  string
	duration  string
	thumbnail string
	skippers  []string
	playlist  *YouTubePlaylist
	dontSkip  bool
}

// NewYouTubeSong gathers the metadata for a song extracted from a YouTube video, and returns
// the song.
func NewYouTubeSong(user, id string, playlist *YouTubePlaylist) (*YouTubeSong, error) {
	var apiResponse *jsonq.JsonQuery
	var err error
	url := fmt.Sprintf("https://www.googleapis.com/youtube/v3/videos?part=snippet,contentDetails&id=%s&key=%s",
		id, os.Getenv("YOUTUBE_API_KEY"))
	if apiResponse, err = PerformGetRequest(url); err != nil {
		return nil, err
	}

	title, _ := apiResponse.String("items", "0", "snippet", "title")
	thumbnail, _ := apiResponse.String("items", "0", "snippet", "thumbnails", "high", "url")
	duration, _ := apiResponse.String("items", "0", "contentDetails", "duration")

	minutes, _ := strconv.ParseInt(duration[2:strings.Index(duration, "M")], 10, 32)
	seconds, _ := strconv.ParseInt(duration[strings.Index(duration, "M")+1:len(duration)-1], 10, 32)
	totalSeconds := int((minutes * 60) + seconds)
	durationString := fmt.Sprintf("%d:%d", minutes, seconds)

	if dj.conf.General.MaxSongDuration == 0 || totalSeconds <= dj.conf.General.MaxSongDuration {
		song := &YouTubeSong{
			submitter: user,
			title:     title,
			id:        id,
			filename:  id + ".m4a",
			duration:  durationString,
			thumbnail: thumbnail,
			skippers:  make([]string, 0),
			playlist:  nil,
			dontSkip:  false,
		}
		dj.queue.AddSong(song)
		return song, nil
	}
	return nil, errors.New("Song exceeds the maximum allowed duration.")
}

// Download downloads the song via youtube-dl if it does not already exist on disk.
// All downloaded songs are stored in ~/.mumbledj/songs and should be automatically cleaned.
func (s *YouTubeSong) Download() error {
	if _, err := os.Stat(fmt.Sprintf("%s/.mumbledj/songs/%s.m4a", dj.homeDir, s.id)); os.IsNotExist(err) {
		cmd := exec.Command("youtube-dl", "--output", fmt.Sprintf(`~/.mumbledj/songs/%s.m4a`, s.id), "--format", "m4a", "--", s.id)
		if err := cmd.Run(); err == nil {
			if dj.conf.Cache.Enabled {
				dj.cache.CheckMaximumDirectorySize()
			}
			return nil
		}
		return errors.New("Song download failed.")
	}
	return nil
}

// Play plays the song. Once the song is playing, a notification is displayed in a text message that features the video
// thumbnail, URL, title, duration, and submitter.
func (s *YouTubeSong) Play() {
	if err := dj.audioStream.Play(fmt.Sprintf("%s/.mumbledj/songs/%s.m4a", dj.homeDir, s.id), dj.queue.OnSongFinished); err != nil {
		panic(err)
	} else {
		if s.playlist == nil {
			message := `
				<table>
					<tr>
						<td align="center"><img src="%s" width=150 /></td>
					</tr>
					<tr>
						<td align="center"><b><a href="http://youtu.be/%s">%s</a> (%s)</b></td>
					</tr>
					<tr>
						<td align="center">Added by %s</td>
					</tr>
				</table>
			`
			dj.client.Self.Channel.Send(fmt.Sprintf(message, s.thumbnail, s.id, s.title,
				s.duration, s.submitter), false)
		} else {
			message := `
				<table>
					<tr>
						<td align="center"><img src="%s" width=150 /></td>
					</tr>
					<tr>
						<td align="center"><b><a href="http://youtu.be/%s">%s</a> (%s)</b></td>
					</tr>
					<tr>
						<td align="center">Added by %s</td>
					</tr>
					<tr>
						<td align="center">From playlist "%s"</td>
					</tr>
				</table>
			`
			dj.client.Self.Channel.Send(fmt.Sprintf(message, s.thumbnail, s.id,
				s.title, s.duration, s.submitter, s.playlist.title), false)
		}
	}
}

// Delete deletes the song from ~/.mumbledj/songs if the cache is disabled.
func (s *YouTubeSong) Delete() error {
	if dj.conf.Cache.Enabled == false {
		filePath := fmt.Sprintf("%s/.mumbledj/songs/%s.m4a", dj.homeDir, s.id)
		if _, err := os.Stat(filePath); err == nil {
			if err := os.Remove(filePath); err == nil {
				return nil
			}
			return errors.New("Error occurred while deleting audio file.")
		}
		return nil
	}
	return nil
}

// AddSkip adds a skip to the skippers slice. If the user is already in the slice, AddSkip
// returns an error and does not add a duplicate skip.
func (s *YouTubeSong) AddSkip(username string) error {
	for _, user := range s.skippers {
		if username == user {
			return errors.New("This user has already skipped the current song.")
		}
	}
	s.skippers = append(s.skippers, username)
	return nil
}

// RemoveSkip removes a skip from the skippers slice. If username is not in slice, an error is
// returned.
func (s *YouTubeSong) RemoveSkip(username string) error {
	for i, user := range s.skippers {
		if username == user {
			s.skippers = append(s.skippers[:i], s.skippers[i+1:]...)
			return nil
		}
	}
	return errors.New("This user has not skipped the song.")
}

// SkipReached calculates the current skip ratio based on the number of users within MumbleDJ's
// channel and the number of usernames in the skippers slice. If the value is greater than or equal
// to the skip ratio defined in the config, the function returns true, and returns false otherwise.
func (s *YouTubeSong) SkipReached(channelUsers int) bool {
	if float32(len(s.skippers))/float32(channelUsers) >= dj.conf.General.SkipRatio {
		return true
	}
	return false
}

// Submitter returns the name of the submitter of the YouTubeSong.
func (s *YouTubeSong) Submitter() string {
	return s.submitter
}

// Title returns the title of the YouTubeSong.
func (s *YouTubeSong) Title() string {
	return s.title
}

// ID returns the id of the YouTubeSong.
func (s *YouTubeSong) ID() string {
	return s.id
}

// Filename returns the filename of the YouTubeSong.
func (s *YouTubeSong) Filename() string {
	return s.filename
}

// Duration returns the duration of the YouTubeSong.
func (s *YouTubeSong) Duration() string {
	return s.duration
}

// Thumbnail returns the thumbnail URL for the YouTubeSong.
func (s *YouTubeSong) Thumbnail() string {
	return s.thumbnail
}

// Playlist returns the playlist type for the YouTubeSong (may be nil).
func (s *YouTubeSong) Playlist() Playlist {
	return *s.playlist
}

// DontSkip returns the DontSkip boolean value for the YouTubeSong.
func (s *YouTubeSong) DontSkip() bool {
	return s.dontSkip
}

// SetDontSkip sets the DontSkip boolean value for the YouTubeSong.
func (s *YouTubeSong) SetDontSkip(value bool) {
	s.dontSkip = value
}

// ----------------
// YOUTUBE PLAYLIST
// ----------------

// YouTubePlaylist holds the metadata for a YouTube playlist.
type YouTubePlaylist struct {
	id    string
	title string
}

// NewYouTubePlaylist gathers the metadata for a YouTube playlist and returns it.
func NewYouTubePlaylist(user, id string) (*YouTubePlaylist, error) {
	var apiResponse *jsonq.JsonQuery
	var err error
	// Retrieve title of playlist
	url := fmt.Sprintf("https://www.googleapis.com/youtube/v3/playlists?part=snippet&id=%s&key=%s",
		id, os.Getenv("YOUTUBE_API_KEY"))
	if apiResponse, err = PerformGetRequest(url); err != nil {
		return nil, err
	}
	title, _ := apiResponse.String("items", "0")

	playlist := &YouTubePlaylist{
		id:    id,
		title: title,
	}

	// Retrieve items in playlist
	url = fmt.Sprintf("https://www.googleapis.com/youtube/v3/playlistItems?part=snippet&maxResults=25&playlistId=%s&key=%s",
		id, os.Getenv("YOUTUBE_API_KEY"))
	if apiResponse, err = PerformGetRequest(url); err != nil {
		return nil, err
	}
	numVideos, _ := apiResponse.Int("pageInfo", "totalResults")
	if numVideos > 25 {
		numVideos = 25
	}

	for i := 0; i < numVideos; i++ {
		index := strconv.Itoa(i)
		videoTitle, _ := apiResponse.String("items", index, "snippet", "title")
		videoID, _ := apiResponse.String("items", index, "snippet", "resourceId", "videoId")
		videoThumbnail, _ := apiResponse.String("items", index, "snippet", "thumbnails", "high", "url")

		// A completely separate API call just to get the duration of a video in a
		// playlist? WHY GOOGLE, WHY?!
		url = fmt.Sprintf("https://www.googleapis.com/youtube/v3/videos?part=contentDetails&id=%s&key=%s",
			videoID, os.Getenv("YOUTUBE_API_KEY"))
		if apiResponse, err = PerformGetRequest(url); err != nil {
			return nil, err
		}
		videoDuration, _ := apiResponse.String("items", "0", "contentDetails", "duration")
		minutes, _ := strconv.ParseInt(videoDuration[2:strings.Index(videoDuration, "M")], 10, 32)
		seconds, _ := strconv.ParseInt(videoDuration[strings.Index(videoDuration, "M")+1:len(videoDuration)-1], 10, 32)
		totalSeconds := int((minutes * 60) + seconds)
		durationString := fmt.Sprintf("%d:%d", minutes, seconds)

		if dj.conf.General.MaxSongDuration == 0 || totalSeconds <= dj.conf.General.MaxSongDuration {
			playlistSong := &YouTubeSong{
				submitter: user,
				title:     videoTitle,
				id:        videoID,
				duration:  durationString,
				thumbnail: videoThumbnail,
				skippers:  make([]string, 0),
				playlist:  playlist,
				dontSkip:  false,
			}
			dj.queue.AddSong(playlistSong)
		}
	}
	return playlist, nil
}

// AddSkip adds a skip to the playlist's skippers slice.
func (p *YouTubePlaylist) AddSkip(username string) error {
	for _, user := range dj.playlistSkips[p.id] {
		if username == user {
			return errors.New("This user has already skipped the current song.")
		}
	}
	dj.playlistSkips[p.id] = append(dj.playlistSkips[p.id], username)
	return nil
}

// RemoveSkip removes a skip from the playlist's skippers slice. If username is not in the slice
// an error is returned.
func (p *YouTubePlaylist) RemoveSkip(username string) error {
	for i, user := range dj.playlistSkips[p.id] {
		if username == user {
			dj.playlistSkips[p.id] = append(dj.playlistSkips[p.id][:i], dj.playlistSkips[p.id][i+1:]...)
			return nil
		}
	}
	return errors.New("This user has not skipped the song.")
}

// DeleteSkippers removes the skippers entry in dj.playlistSkips.
func (p *YouTubePlaylist) DeleteSkippers() {
	delete(dj.playlistSkips, p.id)
}

// SkipReached calculates the current skip ratio based on the number of users within MumbleDJ's
// channel and the number of usernames in the skippers slice. If the value is greater than or equal
// to the skip ratio defined in the config, the function returns true, and returns false otherwise.
func (p *YouTubePlaylist) SkipReached(channelUsers int) bool {
	if float32(len(dj.playlistSkips[p.id]))/float32(channelUsers) >= dj.conf.General.PlaylistSkipRatio {
		return true
	}
	return false
}

// ID returns the id of the YouTubePlaylist.
func (p *YouTubePlaylist) ID() string {
	return p.id
}

// Title returns the title of the YouTubePlaylist.
func (p *YouTubePlaylist) Title() string {
	return p.title
}

// -----------
// YOUTUBE API
// -----------

// PerformGetRequest does all the grunt work for a YouTube HTTPS GET request.
func PerformGetRequest(url string) (*jsonq.JsonQuery, error) {
	jsonString := ""

	if response, err := http.Get(url); err == nil {
		defer response.Body.Close()
		if response.StatusCode == 200 {
			if body, err := ioutil.ReadAll(response.Body); err == nil {
				jsonString = string(body)
			}
		} else {
			if response.StatusCode == 403 {
				return nil, errors.New("Invalid API key supplied.")
			}
			return nil, errors.New("Invalid YouTube ID supplied.")
		}
	} else {
		return nil, errors.New("An error occurred while receiving HTTP GET response.")
	}

	jsonData := map[string]interface{}{}
	decoder := json.NewDecoder(strings.NewReader(jsonString))
	decoder.Decode(&jsonData)
	jq := jsonq.NewQuery(jsonData)

	return jq, nil
}
