module Feature.UploadQueue.View exposing
    ( Config
    , view
    )

{-| Upload-panel rendering over the pure queue state.

The view reads the queue model and reports user intent through the
caller-supplied config; it performs no effects and owns no state. The
coordinator wires the config to its own messages. Wording and structure
are unchanged from the previous inline panel.
-}

import Feature.UploadQueue.Queue as Queue exposing (Entry)
import Html exposing (Html, button, div, h2, input, label, li, p, span, text, ul)
import Html.Attributes as Attr exposing (attribute, class, disabled, id, placeholder, title, type_, value)
import Html.Events exposing (onClick, onInput)


{-| Intent callbacks. The coordinator maps each to its own message.
-}
type alias Config msg =
    { onClosePanel : msg
    , onTagsChanged : String -> msg
    , onSourceChanged : String -> msg
    , onArtistChanged : String -> msg
    , onBrowse : msg
    , onViewPost : String -> msg
    , onDismiss : Int -> msg
    , onClearFinished : msg
    }


{-| The panel stays inspectable while locked (finished entries keep
their thumbnails and post links), but picking new files needs writes.
-}
view : Config msg -> Bool -> Queue.Model -> Html msg
view config writesEnabled model =
    if not model.open then
        text ""

    else
        div [ class "upload-panel", attribute "role" "dialog", attribute "aria-label" "Upload images" ]
            [ div [ class "panel-header" ]
                [ h2 [ class "panel-title", id "upload-panel-title", attribute "tabindex" "-1" ] [ text "Upload" ]
                , button [ class "button button-quiet", type_ "button", onClick config.onClosePanel, attribute "aria-label" "Close upload panel" ] [ text "×" ]
                ]
            , div [ class "panel-body" ]
                [ div [ class "panel-section" ]
                    [ div [ class "upload-field" ]
                        [ label [ class "upload-label", attribute "for" "upload-tags" ] [ text "Tags (shared)" ]
                        , input
                            [ class "upload-input"
                            , id "upload-tags"
                            , type_ "text"
                            , placeholder "cat, demo"
                            , value model.tagsDraft
                            , onInput config.onTagsChanged
                            ]
                            []
                        ]
                    , div [ class "upload-field" ]
                        [ label [ class "upload-label", attribute "for" "upload-source" ] [ text "Source (shared, optional)" ]
                        , input
                            [ class "upload-input"
                            , id "upload-source"
                            , type_ "text"
                            , placeholder "imports/sample.jpg"
                            , value model.sourceDraft
                            , onInput config.onSourceChanged
                            ]
                            []
                        ]
                    , div [ class "upload-field" ]
                        [ label [ class "upload-label", attribute "for" "upload-artist" ] [ text "Artist (shared, optional)" ]
                        , input
                            [ class "upload-input"
                            , id "upload-artist"
                            , type_ "text"
                            , placeholder "Kura"
                            , value model.artistDraft
                            , onInput config.onArtistChanged
                            ]
                            []
                        ]
                    , p [ class "panel-note" ] [ text "Shared fields apply to files you pick next. JPEG, PNG, GIF, or WebP up to 25 MiB each." ]
                    , if writesEnabled then
                        button [ class "button", type_ "button", id "upload-browse", onClick config.onBrowse ] [ text "Choose files…" ]

                      else
                        div []
                            [ button [ class "button", type_ "button", id "upload-browse", disabled True, title "Unlock to upload images." ] [ text "Choose files…" ]
                            , p [ class "panel-note" ] [ text "Unlock to upload images." ]
                            ]
                    ]
                , div [ class "panel-section" ]
                    [ h2 [ class "panel-section-title" ] [ text (queueHeading model) ]
                    , queueList config model
                    , if List.any Queue.isDone model.entries then
                        button [ class "button button-quiet", type_ "button", onClick config.onClearFinished ] [ text "Clear finished" ]

                      else
                        text ""
                    ]
                ]
            ]


queueHeading : Queue.Model -> String
queueHeading model =
    let
        total =
            List.length model.entries

        pending =
            Queue.pendingCount model
    in
    if total == 0 then
        "Queue"

    else if pending == 0 then
        "Queue (" ++ String.fromInt total ++ " finished)"

    else
        "Queue (" ++ String.fromInt pending ++ " of " ++ String.fromInt total ++ " pending)"


queueList : Config msg -> Queue.Model -> Html msg
queueList config model =
    if List.isEmpty model.entries then
        p [ class "panel-note" ] [ text "No files picked yet." ]

    else
        ul [ class "upload-queue" ] (List.map (entryView config) model.entries)


entryView : Config msg -> Entry -> Html msg
entryView config entry =
    li [ class "upload-item" ]
        [ div [ class "upload-item-head" ]
            [ span [ class "upload-name", title entry.name ] [ text entry.name ]
            , span [ class "upload-size" ] [ text (Queue.formatBytes entry.size) ]
            , span [ class "upload-phase", attribute "data-phase" (Queue.phaseLabel entry.phase) ] [ text (Queue.phaseLabel entry.phase) ]
            ]
        , phaseDetail entry
        , div [ class "upload-item-actions" ]
            [ case entry.phase of
                Queue.Uploaded info ->
                    button [ class "button button-quiet", type_ "button", onClick (config.onViewPost info.postId) ] [ text "View" ]

                _ ->
                    text ""
            , if Queue.isDismissible entry then
                button [ class "button button-quiet", type_ "button", onClick (config.onDismiss entry.id), attribute "aria-label" ("Dismiss " ++ entry.name) ] [ text "Dismiss" ]

              else
                button [ class "button button-quiet", type_ "button", disabled True, title "Cannot dismiss while the upload is in flight.", attribute "aria-label" ("Dismiss " ++ entry.name ++ " (unavailable while uploading)") ] [ text "Dismiss" ]
            ]
        ]


phaseDetail : Entry -> Html msg
phaseDetail entry =
    case entry.phase of
        Queue.Queued ->
            text ""

        Queue.Uploading sending ->
            let
                fraction =
                    if sending.size <= 0 then
                        0

                    else
                        clamp 0 100 (sending.sent * 100 // sending.size)
            in
            div [ class "upload-progress" ]
                [ Html.progress
                    [ class "upload-bar"
                    , value (String.fromInt sending.sent)
                    , Attr.max (String.fromInt (max 1 sending.size))
                    , attribute "aria-label" ("Upload progress for " ++ entry.name)
                    ]
                    []
                , span [ class "upload-percent" ] [ text (String.fromInt fraction ++ "%") ]
                ]

        Queue.Saving _ ->
            p [ class "upload-note" ] [ text "Saving to the Library…" ]

        Queue.Uploaded info ->
            let
                detail =
                    case info.thumb of
                        Queue.ThumbWaiting _ ->
                            "Thumbnail: processing…"

                        Queue.ThumbReady ->
                            "Thumbnail: ready"

                        Queue.ThumbFailed message ->
                            message
            in
            div []
                [ p [ class "upload-note" ] [ text ("Saved as post #" ++ info.postId ++ ".") ]
                , p [ class "upload-note" ] [ text detail ]
                ]

        Queue.Rejected message ->
            p [ class "upload-error", attribute "role" "alert" ] [ text message ]

        Queue.Failed message ->
            p [ class "upload-error", attribute "role" "alert" ] [ text message ]
