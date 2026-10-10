module Feature.Inspector exposing (Config, view)

import Api.Post
import Domain.Collection exposing (Collection)
import Domain.Post exposing (PostDetail, PostSummary, TagRevision)
import Html exposing (Html, a, aside, button, dd, details, div, dl, dt, form, input, label, li, option, p, select, span, summary, text, ul)
import Html.Attributes exposing (attribute, class, classList, disabled, href, placeholder, rel, selected, style, target, title, type_, value)
import Html.Events exposing (on, onClick, onInput, onSubmit)
import Json.Decode as Decode
import Set exposing (Set)
import Ui.Button
import Ui.Icon
import Ui.Media
import Ui.Panel as Panel exposing (Presentation)


type alias Config msg =
    { apiBase : String
    , presentation : Presentation
    , active : Maybe PostSummary
    , detail : Maybe PostDetail
    , detailLoading : Bool
    , detailError : Maybe String
    , selectedCount : Int
    , showPreview : Bool
    , commonTags : List String
    , missing : Set String
    , onTag : String -> Bool -> msg
    , onClear : msg
    , onClose : msg
    , collections : List Collection
    , activeCollection : Maybe String
    , onSelectCollection : String -> msg
    , onFavorite : msg
    , onAddToCollection : msg
    , onMediaError : String -> msg
    , writesEnabled : Bool
    , tagAdd : String
    , tagRemove : String
    , tagStatus : Maybe String
    , tagSaving : Bool
    , onTagAdd : String -> msg
    , onTagRemove : String -> msg
    , onSaveTags : msg
    , scoreDraft : String
    , reactionSaving : Bool
    , reactionStatus : Maybe String
    , onScoreDraft : String -> msg
    , onStepScore : Int -> msg
    , onSaveScore : msg
    , onRevert : Int -> msg
    , onConfirmRevert : Int -> msg
    , onCancelRevert : msg
    , revertConfirm : Maybe Int
    , revertPending : Maybe Int
    , revertStatus : Maybe String
    }


view : Config msg -> Html msg
view config =
    aside [ class (Panel.presentationClass "inspector" config.presentation), attribute "aria-label" "Image details" ]
        [ Panel.header
            (if config.selectedCount > 0 then
                String.fromInt config.selectedCount ++ " selected"

             else
                "Image details"
            )
            config.presentation
            config.onClose
        , div [ class "panel-body" ]
            (case config.active of
                Nothing ->
                    [ p [ class "inspector-empty" ] [ text "Select an image to see its details." ] ]

                Just post ->
                    [ identitySection config post
                    , reactionSection config post
                    , Panel.section
                        (if config.selectedCount > 1 then
                            "Shared tags"

                         else
                            "Tags"
                        )
                        (tagList config
                            (if config.selectedCount > 1 then
                                config.commonTags

                             else
                                detailTags config post
                            )
                            ++ [ tagEditor config ]
                        )
                    , detailSection config post
                    , details [ class "panel-section inspector-disclosure" ]
                        (summary [] [ text "History" ] :: historyList config)
                    , selectionSection config
                    ]
            )
        ]


detailSection : Config msg -> PostSummary -> Html msg
detailSection config post =
    case config.detail of
        Just detail ->
            if detail.id == post.id then
                div []
                    [ if detail.source == "" then
                        text ""

                      else
                        Panel.section "Source"
                            [ if String.startsWith "https://" detail.source || String.startsWith "http://" detail.source then
                                a [ class "source-link", href detail.source, target "_blank", rel "noopener noreferrer" ]
                                    [ span [] [ text detail.source ], Ui.Icon.view "external" ]

                              else
                                p [ class "source-text" ] [ text detail.source ]
                            ]
                    , details [ class "panel-section inspector-disclosure" ]
                        [ summary [] [ text "File details" ]
                        , dl [ class "facts" ]
                            ([ div [ class "fact" ]
                                [ dt [] [ text "Original" ]
                                , dd [] [ a [ href (Api.Post.mediaUrl config.apiBase post.originalUrl), target "_blank", rel "noopener noreferrer" ] [ text "Open original ↗" ] ]
                                ]
                             , fact "Dimensions" (String.fromInt post.width ++ " × " ++ String.fromInt post.height)
                             , fact "Type" post.mediaType
                             ]
                                ++ (if detail.artist == "" then
                                        []

                                    else
                                        [ fact "Artist" detail.artist ]
                                   )
                                ++ (if detail.hash == "" then
                                        []

                                    else
                                        [ fact "Hash" detail.hash ]
                                   )
                                ++ (if detail.fileSize <= 0 then
                                        []

                                    else
                                        [ fact "Size" (fileSize detail.fileSize) ]
                                   )
                                ++ (if detail.createdAt == "" then
                                        []

                                    else
                                        [ fact "Added" (String.left 10 detail.createdAt) ]
                                   )
                            )
                        ]
                    ]

            else
                detailPlaceholder config

        Nothing ->
            detailPlaceholder config


detailPlaceholder : Config msg -> Html msg
detailPlaceholder config =
    if config.detailLoading then
        p [ class "panel-note inspector-loading", attribute "role" "status" ] [ text "Loading details…" ]

    else
        case config.detailError of
            Just message ->
                p [ class "panel-note status-error" ] [ text message ]

            Nothing ->
                text ""


detailTags : Config msg -> PostSummary -> List String
detailTags config post =
    case config.detail of
        Just detail ->
            if detail.id == post.id then
                detail.tags

            else
                post.tags

        Nothing ->
            post.tags


identitySection : Config msg -> PostSummary -> Html msg
identitySection config post =
    div [ class "panel-section inspector-identity" ]
        [ if config.showPreview then
            div [ class "inspector-preview", style "aspect-ratio" "4 / 3" ]
                [ Ui.Media.image [ class "inspector-preview-image" ]
                    { url = Api.Post.mediaUrl config.apiBase post.previewUrl
                    , postId = post.id
                    , missing = Set.member post.id config.missing
                    , onError = config.onMediaError
                    }
                ]

          else
            text ""
        , div [ class "identity-row" ]
            [ div []
                [ p [ class "inspector-id" ] [ text ("#" ++ post.id) ]
                , p [ class "identity-meta" ] [ text (String.fromInt post.width ++ " × " ++ String.fromInt post.height ++ " · " ++ mediaLabel post.mediaType) ]
                ]
            , favoriteButton config
            ]
        ]


favoriteButton : Config msg -> Html msg
favoriteButton config =
    let
        favorite =
            config.detail |> Maybe.map .favorite |> Maybe.withDefault False
    in
    Ui.Button.icon "heart"
        [ class "favorite-button" ]
        { label =
            if favorite then
                "Remove favorite"

            else
                "Favorite"
        , key = Nothing
        , onPress =
            if config.writesEnabled && not config.reactionSaving && config.detail /= Nothing then
                Just config.onFavorite

            else
                Nothing
        , pressed = Just favorite
        , hint = Just "Toggle favorite for the selection (F)"
        }


selectionSection : Config msg -> Html msg
selectionSection config =
    details [ class "panel-section inspector-disclosure" ]
        [ summary [] [ text "Selection actions" ]
        , if List.isEmpty config.collections then
            p [ class "panel-note" ] [ text "Create a collection in the library to add images." ]

          else
            div [ class "collection-target" ]
                [ select [ class "compact-input", attribute "aria-label" "Target collection", onInput config.onSelectCollection, disabled (not config.writesEnabled) ]
                    (List.map
                        (\collection -> option [ value collection.id, selected (config.activeCollection == Just collection.id) ] [ text collection.name ])
                        config.collections
                    )
                , button [ class "button", type_ "button", onClick config.onAddToCollection, disabled (not config.writesEnabled || config.activeCollection == Nothing) ] [ text "Add to collection" ]
                ]
        , if config.selectedCount > 0 then
            button [ class "button button-quiet", type_ "button", onClick config.onClear ] [ text "Clear selection" ]

          else
            text ""
        ]


tagList : Config msg -> List String -> List (Html msg)
tagList config tags =
    if List.isEmpty tags then
        []

    else
        [ ul [ class "tag-list" ] (List.map (tagItem config) tags) ]


tagEditor : Config msg -> Html msg
tagEditor config =
    form [ class "tag-editor", onSubmit config.onSaveTags ]
        [ input
            [ class "compact-input"
            , Html.Attributes.id "tags-to-add"
            , type_ "text"
            , placeholder "Add tags…"
            , value config.tagAdd
            , onInput config.onTagAdd
            , attribute "aria-label" "Tags to add"
            , title "Separate multiple tags with commas; Enter to save"
            , attribute "autocomplete" "off"
            , disabled (not config.writesEnabled || config.tagSaving)
            ]
            []
        , details [ class "tag-remove-disclosure" ]
            [ summary [] [ text "Remove tags" ]
            , input
                [ class "compact-input"
                , type_ "text"
                , placeholder "Tags to remove…"
                , value config.tagRemove
                , onInput config.onTagRemove
                , attribute "aria-label" "Tags to remove"
                , disabled (not config.writesEnabled || config.tagSaving)
                ]
                []
            ]
        , if config.tagSaving || String.trim config.tagAdd /= "" || String.trim config.tagRemove /= "" then
            button [ class "button", type_ "submit", disabled (config.tagSaving || not config.writesEnabled) ]
                [ text
                    (if config.tagSaving then
                        "Saving…"

                     else
                        "Save tags"
                    )
                ]

          else
            text ""
        , case config.tagStatus of
            Just status ->
                p [ class "panel-note", attribute "role" "status" ] [ text status ]

            Nothing ->
                text ""
        ]


reactionSection : Config msg -> PostSummary -> Html msg
reactionSection config post =
    case config.detail of
        Just detail ->
            if detail.id == post.id then
                div [ class "panel-section score-section" ]
                    [ form [ class "score-form", onSubmit config.onSaveScore ]
                        [ label [ Html.Attributes.for "post-score" ] [ text "Score" ]
                        , div [ class "score-stepper" ]
                            [ button [ class "score-step", type_ "button", onClick (config.onStepScore -1), disabled (config.reactionSaving || not config.writesEnabled || (String.toInt config.scoreDraft |> Maybe.withDefault 0) <= 0), attribute "aria-label" "Decrease score" ] [ text "−" ]
                            , input
                                [ class "compact-input score-input"
                                , Html.Attributes.id "post-score"
                                , type_ "number"
                                , attribute "min" "0"
                                , value config.scoreDraft
                                , onInput config.onScoreDraft
                                , attribute "aria-label" "Score"
                                , title "Score for the selection; Enter to save"
                                , disabled (config.reactionSaving || not config.writesEnabled)
                                ]
                                []
                            , button [ class "score-step", type_ "button", onClick (config.onStepScore 1), disabled (config.reactionSaving || not config.writesEnabled), attribute "aria-label" "Increase score" ] [ text "+" ]
                            ]
                        , if config.scoreDraft /= String.fromInt detail.score then
                            button [ class "button", type_ "submit", disabled (config.reactionSaving || not config.writesEnabled) ] [ text "Save" ]

                          else
                            text ""
                        ]
                    , case config.reactionStatus of
                        Just status ->
                            p [ class "panel-note", attribute "role" "status" ] [ text status ]

                        Nothing ->
                            text ""
                    ]

            else
                text ""

        Nothing ->
            text ""


mediaLabel : String -> String
mediaLabel mediaType =
    String.replace "image/" "" mediaType |> String.toUpper


fileSize : Int -> String
fileSize bytes =
    if bytes >= 1048576 then
        String.fromFloat (toFloat (round (toFloat bytes / 104857.6)) / 10) ++ " MB"

    else
        String.fromInt (max 1 (bytes // 1024)) ++ " KB"


historyList : Config msg -> List (Html msg)
historyList config =
    case config.detail of
        Just detail ->
            if List.isEmpty detail.history then
                [ p [ class "panel-note" ] [ text "No tag revisions yet." ] ]

            else
                [ ul [ class "history-list" ] (List.map (historyItem config) detail.history)
                , case config.revertStatus of
                    Just status ->
                        p [ class "panel-note status-info" ] [ text status ]

                    Nothing ->
                        text ""
                , case config.revertPending of
                    Just v ->
                        p [ class "panel-note" ] [ text ("Reverting to v" ++ String.fromInt v ++ "…") ]

                    Nothing ->
                        text ""
                ]

        Nothing ->
            [ p [ class "panel-note" ] [ text "Select a post to load history." ] ]


historyItem : Config msg -> TagRevision -> Html msg
historyItem config revision =
    let
        isConfirming =
            config.revertConfirm == Just revision.version

        isPending =
            config.revertPending == Just revision.version

        canRevert =
            revision.revertible

        beforeTags =
            revision.removedTags

        afterTags =
            revision.addedTags

        targetTags =
            revision.targetTags
    in
    li [ class "history-item", classList [ ( "is-legacy", not canRevert ), ( "is-pending", isPending ) ] ]
        [ div [ class "history-header" ]
            [ span [ class "history-version" ] [ text ("v" ++ String.fromInt revision.version) ]
            , span [ class "history-kind" ] [ text revision.kind ]
            , span [ class "history-time" ] [ text revision.createdAt ]
            , if not canRevert then
                span [ class "history-legacy", title "Legacy revision without stored tag state" ] [ text "legacy" ]

              else
                text ""
            ]
        , div [ class "history-diff" ]
            [ div [ class "history-chips before" ]
                (if List.isEmpty beforeTags then
                    [ span [ class "panel-note" ] [ text "No removed tags" ] ]

                 else
                    [ span [ class "history-label" ] [ text "Removed:" ]
                    , ul [ class "tag-list" ] (List.map chipRemoved beforeTags)
                    ]
                )
            , div [ class "history-chips after" ]
                (if List.isEmpty afterTags then
                    [ span [ class "panel-note" ] [ text "No added tags" ] ]

                 else
                    [ span [ class "history-label" ] [ text "Added:" ]
                    , ul [ class "tag-list" ] (List.map chipAdded afterTags)
                    ]
                )
            , div [ class "history-chips result" ]
                (case targetTags of
                    Nothing ->
                        [ p [ class "panel-note status-error" ] [ text "No stored tag state — cannot revert" ] ]

                    Just tags ->
                        if List.isEmpty tags then
                            [ span [ class "history-label" ] [ text "Result:" ]
                            , span [ class "panel-note" ] [ text "No tags" ]
                            ]

                        else
                            [ span [ class "history-label" ] [ text "Result:" ]
                            , ul [ class "tag-list" ] (List.map chipResult tags)
                            ]
                )
            ]
        , div [ class "history-actions" ]
            (if not canRevert then
                [ button
                    [ class "button"
                    , type_ "button"
                    , disabled True
                    , title "Legacy revision cannot be reverted"
                    ]
                    [ text "Revert" ]
                ]

             else if isPending then
                [ button
                    [ class "button is-pending"
                    , type_ "button"
                    , disabled True
                    ]
                    [ text "Reverting…" ]
                ]

             else if isConfirming then
                [ p [ class "panel-note" ] [ text ("Revert to v" ++ String.fromInt revision.version ++ "?") ]
                , button
                    [ class "button"
                    , type_ "button"
                    , onClick (config.onConfirmRevert revision.version)
                    , disabled (config.tagSaving || not config.writesEnabled)
                    ]
                    [ text "Confirm" ]
                , button
                    [ class "button button-quiet"
                    , type_ "button"
                    , onClick config.onCancelRevert
                    , disabled config.tagSaving
                    ]
                    [ text "Cancel" ]
                ]

             else
                [ button
                    [ class "button"
                    , type_ "button"
                    , onClick (config.onRevert revision.version)
                    , disabled (config.tagSaving || not config.writesEnabled || config.revertPending /= Nothing)
                    ]
                    [ text "Revert" ]
                ]
            )
        ]


chipAdded : String -> Html msg
chipAdded tag =
    li [] [ span [ class "chip chip-added", title ("Added " ++ tag) ] [ text tag ] ]


chipRemoved : String -> Html msg
chipRemoved tag =
    li [] [ span [ class "chip chip-removed", title ("Removed " ++ tag) ] [ text tag ] ]


chipResult : String -> Html msg
chipResult tag =
    li [] [ span [ class "chip chip-result", title tag ] [ text tag ] ]


tagItem : Config msg -> String -> Html msg
tagItem config tag =
    li []
        [ button
            [ class "chip"
            , type_ "button"
            , title ("Filter by " ++ tag ++ " (Alt+click to exclude)")
            , on "click" (Decode.map (config.onTag tag) (Decode.field "altKey" Decode.bool))
            ]
            [ text tag ]
        ]


fact : String -> String -> Html msg
fact label value =
    div [ class "fact" ] [ dt [] [ text label ], dd [] [ text value ] ]
